package search

import (
	"context"
	"fmt"
	"math"
	"sort"
	"sync"

	"pinpoint/internal/acoustic"
	"pinpoint/internal/geo"
	"pinpoint/internal/tracks"
)

// SilentInterval is a reviewed absence of audible aircraft, with an explicit
// assumed audibility radius. It must not be inferred from detector misses.
type SilentInterval struct {
	Start    float64 `json:"start_s"`
	End      float64 `json:"end_s"`
	MaxSlant float64 `json:"max_slant_m"`
}

func validateStaged(c Config, s *acoustic.Spectrum) error {
	if !geo.Finite(c.DenseGrid) || c.DenseGrid < 0 {
		return fmt.Errorf("invalid dense grid")
	}
	if c.Staged {
		if len(c.Events) < 2 || c.Grid > 200 || c.DenseGrid < 5 || c.DenseGrid > 100 || c.DenseGrid > c.Grid || c.Refine > c.DenseGrid {
			return fmt.Errorf("staged joint search requires at least two measured events, coarse grid <=200 m, dense grid 5–100 m, and refinement no coarser than the dense grid")
		}
		for i, e := range c.Events {
			if !e.Partial && e.CPATolerance <= 0 {
				return fmt.Errorf("event %d needs delayed-CPA uncertainty, or must be marked as a partial flyover without a measured CPA", i+1)
			}
		}
	}
	for _, w := range c.Silence {
		if !geo.Finite(w.Start) || !geo.Finite(w.End) || !geo.Finite(w.MaxSlant) || w.Start < s.Time(0) || w.End > s.Time(len(s.Rows)-1) || w.End <= w.Start || w.MaxSlant <= 0 || w.MaxSlant > 10000 {
			return fmt.Errorf("invalid reviewed silence interval (audibility radius must be <=10 km)")
		}
		for _, e := range c.Events {
			if w.Start < e.End && w.End > e.Start {
				return fmt.Errorf("silence interval overlaps an observed flyover")
			}
		}
	}
	return nil
}

func eventAtClock(e acoustic.Event, a tracks.Approach, recordStart float64) bool {
	if e.Partial {
		return true
	}
	if e.CPATolerance > 0 {
		return math.Abs(a.Heard-recordStart-e.CPATime) <= e.CPATolerance
	}
	return a.Heard >= recordStart+e.Start-10 && a.Heard <= recordStart+e.End+10
}

func eventPossible(e acoustic.Event, a tracks.Approach, start, uncertainty float64) bool {
	if e.CPATolerance > 0 {
		return math.Abs(a.Heard-start-e.CPATime) <= uncertainty+e.CPATolerance
	}
	return a.Heard >= start-uncertainty+e.Start-10 && a.Heard <= start+uncertainty+e.End+10
}

type cpaInterval struct {
	id     string
	lo, hi float64
}

// Shared delayed-CPA intervals encode the pair offset constraint without using
// acoustic brightness. Unknown absolute time is one common clock interval.
func geometryIntervals(p observer, c Config, ts []tracks.Track, start, slack float64) [][]cpaInterval {
	eligible := make([][]cpaInterval, len(c.Events))
	for _, t := range ts {
		a, ok := t.CPA(p.pos, c.SoundSpeed, c.MaxGap)
		if !ok {
			continue
		}
		// Automatic partial events commonly share the same conservative range.
		// Compute that trajectory's arrival intervals once for each radius.
		var partialWindows map[float64][]tracks.ArrivalInterval
		for i, e := range c.Events {
			if e.Partial {
				// Only require possible received sound during the observed window.
				// No center/CPA is inferred from a partial curve. This conservative
				// prefilter keeps every clock that could overlap the trajectory;
				// spectral scoring checks full coverage, shape and range afterward.
				radius := e.MaxSlant + slack
				if partialWindows == nil {
					partialWindows = map[float64][]tracks.ArrivalInterval{}
				}
				windows, cached := partialWindows[radius]
				if !cached {
					windows = t.WithinRadius(p.pos, radius, c.SoundSpeed, c.MaxGap)
					partialWindows[radius] = windows
				}
				for _, w := range windows {
					lo := math.Max(-c.ClockUncertainty, w.Start-start-e.End-slack/c.SoundSpeed)
					hi := math.Min(c.ClockUncertainty, w.End-start-e.Start+slack/c.SoundSpeed)
					if lo <= hi {
						eligible[i] = append(eligible[i], cpaInterval{t.ID, lo, hi})
					}
				}
				continue
			}
			if a.Speed < e.MinSpeed || a.Speed > e.MaxSpeed || a.Distance < math.Max(0, e.MinSlant-slack) || a.Distance > e.MaxSlant+slack {
				continue
			}
			uncertainty := e.CPATolerance + slack/math.Max(15, e.MinSpeed) + slack/c.SoundSpeed
			offset := a.Heard - start - e.CPATime
			lo, hi := math.Max(-c.ClockUncertainty, offset-uncertainty), math.Min(c.ClockUncertainty, offset+uncertainty)
			if lo <= hi {
				eligible[i] = append(eligible[i], cpaInterval{t.ID, lo, hi})
			}
		}
	}
	return eligible
}

// At least one interval start belongs to every nonempty intersection. Test
// those times with bipartite matching instead of exponential flight permutations.
func sharedCPA(eligible [][]cpaInterval) bool {
	if len(eligible) == 0 {
		return false
	}
	var starts []float64
	for _, options := range eligible {
		if len(options) == 0 {
			return false
		}
		for _, a := range options {
			starts = append(starts, a.lo)
		}
	}
	sort.Float64s(starts)
	for k, clock := range starts {
		if k > 0 && clock == starts[k-1] {
			continue
		}
		owner := map[string]int{}
		var assign func(int, map[string]bool) bool
		assign = func(event int, seen map[string]bool) bool {
			for _, a := range eligible[event] {
				if clock < a.lo || clock > a.hi || seen[a.id] {
					continue
				}
				seen[a.id] = true
				old, used := owner[a.id]
				if !used || assign(old, seen) {
					owner[a.id] = event
					return true
				}
			}
			return false
		}
		all := true
		for event := range eligible {
			if !assign(event, map[string]bool{}) {
				all = false
				break
			}
		}
		if all {
			return true
		}
	}
	return false
}

// Convert all predicted audible arrivals to forbidden recording-start ranges.
// This is computed once per observer, rather than once per clock sample.
func silenceExclusions(c Config, ts []tracks.Track, p observer) []tracks.ArrivalInterval {
	var out []tracks.ArrivalInterval
	for _, w := range c.Silence {
		for _, t := range ts {
			for _, heard := range t.WithinRadius(p.pos, w.MaxSlant, c.SoundSpeed, c.MaxGap) {
				out = append(out, tracks.ArrivalInterval{Start: heard.Start - w.End, End: heard.End - w.Start})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Start < out[j].Start })
	merged := out[:0]
	for _, w := range out {
		if len(merged) > 0 && w.Start < merged[len(merged)-1].End {
			merged[len(merged)-1].End = math.Max(merged[len(merged)-1].End, w.End)
		} else {
			merged = append(merged, w)
		}
	}
	return merged
}
func silenceClockAllows(exclusions []tracks.ArrivalInterval, recordStart float64) bool {
	i := sort.Search(len(exclusions), func(i int) bool { return exclusions[i].End > recordStart })
	return i == len(exclusions) || recordStart <= exclusions[i].Start
}
func silenceAllows(c Config, ts []tracks.Track, p observer, recordStart float64) bool {
	return silenceClockAllows(silenceExclusions(c, ts, p), recordStart)
}

// A whole coarse cell survives if its center satisfies relaxed geometric
// bounds. Every surviving region is then covered at the requested dense spacing;
// no acoustic threshold or top-eight ranking is used to discard coarse cells.
type stagedCell struct {
	lat, lon float64
	mask     uint64
}

func stagedLocations(ctx context.Context, c Config, spectrum *acoustic.Spectrum, points []observer, ts []tracks.Track, start float64, pool *WorkerPool, progress func(string, int, int), cellRadius func(observer) float64) ([]stagedCell, int, error) {
	radius := 0.0
	for _, e := range c.Events {
		radius = math.Max(radius, e.MaxSlant+2*c.Grid)
	}
	index := newTrackIndex(ts, radius)
	observations := make([][]scoreObservation, len(c.Events))
	if c.CurveEnvelope {
		for i, e := range c.Events {
			observations[i] = prepareFoldObservations(eventSpectrum(c, spectrum, i), e, c.ObservationFold)
		}
	}
	type cell struct {
		p      observer
		radius float64
	}
	jobs := make(chan cell)
	results := make(chan observer, c.Workers)
	var wg sync.WaitGroup
	if progress != nil {
		progress("geometry", 0, len(points))
	}
	for n := 0; n < c.Workers; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for cell := range jobs {
				p := cell.p
				if !pool.acquire(ctx) {
					continue
				}
				nearby := index.nearby(p.pos)
				slack := c.Grid
				if c.CurveEnvelope && geo.Finite(cell.radius) {
					slack = math.Max(slack, cell.radius)
					if slack > 2*c.Grid {
						nearby = nearbyTracks(p.pos, ts, index.envelopes, radius-2*c.Grid+slack)
					}
				}
				eligible := geometryIntervals(p, c, nearby, start, slack)
				ok := sharedCPA(eligible)
				if c.CurveEnvelope && math.IsInf(cell.radius, 1) {
					ok = true
					p.clockMask = ^uint64(0)
				}
				if ok && c.CurveEnvelope {
					p.clockMask = curveCellClocks(p, cell.radius, c, observations, nearby, start, eligible)
					ok = p.clockMask != 0
				}
				pool.release()
				if !ok {
					p.height = math.NaN()
				}
				results <- p
			}
		}()
	}
	go func() {
		defer close(jobs)
		for _, p := range points {
			radius := 0.0
			if c.CurveEnvelope {
				radius = cellRadius(p)
			}
			select {
			case jobs <- cell{p, radius}:
			case <-ctx.Done():
				return
			}
		}
	}()
	go func() { wg.Wait(); close(results) }()
	var survivors []observer
	done := 0
	for p := range results {
		done++
		if geo.Finite(p.height) {
			survivors = append(survivors, p)
		}
		if progress != nil {
			progress("geometry", done, len(points))
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	mask, err := geo.NewMask(c.RegionMask)
	if err != nil {
		return nil, 0, err
	}
	dy := c.DenseGrid / 111320
	dx := dy / math.Cos((c.Bounds.South+c.Bounds.North)*math.Pi/360)
	seen := map[[2]int]bool{}
	clocks := map[[2]int]uint64{}
	limit := c.MaxGridPoints
	if limit == 0 {
		limit = 250000
	}
	padY, padX := c.Grid/111320, c.Grid/(111320*math.Cos((c.Bounds.South+c.Bounds.North)*math.Pi/360))
	for _, p := range survivors {
		if err := ctx.Err(); err != nil {
			return nil, 0, err
		}
		for y := max(0, int(math.Floor((p.lat-padY-c.Bounds.South)/dy))); y <= int(math.Ceil((p.lat+padY-c.Bounds.South)/dy)); y++ {
			for x := max(0, int(math.Floor((p.lon-padX-c.Bounds.West)/dx))); x <= int(math.Ceil((p.lon+padX-c.Bounds.West)/dx)); x++ {
				key := [2]int{y, x}
				if seen[key] {
					clocks[key] |= p.clockMask
					continue
				}
				lat, lon := c.Bounds.South+float64(y)*dy, c.Bounds.West+float64(x)*dx
				if !c.Bounds.Contains(lat, lon) || !mask.Contains(lat, lon) {
					continue
				}
				seen[key] = true
				clocks[key] = p.clockMask
				if len(seen) > limit {
					return nil, 0, fmt.Errorf("dense surviving regions exceed %d points; narrow the measured bounds or search area (no regions were silently dropped)", limit)
				}
			}
		}
	}
	keys := make([][2]int, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		return keys[i][0] < keys[j][0] || keys[i][0] == keys[j][0] && keys[i][1] < keys[j][1]
	})
	out := make([]stagedCell, 0, len(keys))
	for _, k := range keys {
		out = append(out, stagedCell{c.Bounds.South + float64(k[0])*dy, c.Bounds.West + float64(k[1])*dx, clocks[k]})
	}
	return out, len(survivors), nil
}
