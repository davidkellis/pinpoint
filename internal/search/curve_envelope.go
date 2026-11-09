package search

import (
	"math"
	"pinpoint/internal/acoustic"
	"pinpoint/internal/geo"
	"pinpoint/internal/tracks"
)

func observerCellRadius(p observer, c Config, terrain *geo.Terrain, geoid *geo.Geoid) float64 {
	dy := c.Grid / 111320
	dx := dy / math.Cos((c.Bounds.South+c.Bounds.North)*math.Pi/360)
	b := geo.Bounds{South: math.Max(c.Bounds.South, p.lat-dy), North: math.Min(c.Bounds.North, p.lat+dy), West: math.Max(c.Bounds.West, p.lon-dx), East: math.Min(c.Bounds.East, p.lon+dx)}
	h := 0.0
	if c.ObserverHeight == nil {
		lo, hi, err := terrain.HeightRange(b)
		if err != nil {
			return math.Inf(1)
		}
		nlo, nhi := 0.0, 0.0
		if geoid != nil {
			nlo, nhi, err = geoid.HeightRange(b)
			if err != nil {
				return math.Inf(1)
			}
		} else {
			nlo, nhi = *c.GeoidOffset, *c.GeoidOffset
		}
		h = math.Max(math.Abs(lo+nlo+c.MicHeight-p.height), math.Abs(hi+nhi+c.MicHeight-p.height))
	}
	lat := math.Min(math.Abs(b.South), math.Abs(b.North))
	if b.South < 0 && b.North > 0 {
		lat = 0
	}
	return 6500000*math.Hypot(dy*math.Pi/180, math.Cos(lat*math.Pi/180)*dx*math.Pi/180) + h + 2
}

// Cells pass if at least one common clock interval could explain every curve.
// Frequency intervals enclose all positions/heights within the coarse cell.
// The subset least-squares lower bound never substitutes for final full scoring.
func curveCellClocks(p observer, radius float64, c Config, observed [][]scoreObservation, ts []tracks.Track, start float64, eligible [][]cpaInterval) uint64 {
	if math.IsInf(radius, 1) {
		return ^uint64(0)
	}
	byID := make(map[string]tracks.Track, len(ts))
	for _, t := range ts {
		byID[t.ID] = t.ForObserver(p.pos, c.SoundSpeed)
	}
	step := clockMaskStep(c)
	mask := uint64(0)
	for bin := 0; bin < 64; bin++ {
		low := -c.ClockUncertainty + float64(bin)*step
		if low > c.ClockUncertainty {
			break
		}
		hi := math.Min(c.ClockUncertainty, low+step)
		possible := true
		for i, e := range c.Events {
			found := false
			for _, option := range eligible[i] {
				from, to := math.Max(low, option.lo), math.Min(hi, option.hi)
				if from > to {
					continue
				}
				if curveRangePossible(byID[option.id], p.pos, radius, e, c, observed[i], start+from, start+to) {
					found = true
					break
				}
			}
			if !found {
				possible = false
				break
			}
		}
		if possible {
			mask |= uint64(1) << bin
		}
		if hi == c.ClockUncertainty {
			break
		}
	}
	return mask
}

func clockMaskStep(c Config) float64 {
	return math.Max(math.Max(4, c.ClockStep), 2*c.ClockUncertainty/64)
}
func clockMaskAllows(mask uint64, c Config, offset float64) bool {
	if mask == 0 {
		return true
	} // legacy searches and final refinement use all clocks
	step := clockMaskStep(c)
	bin := int(math.Floor((offset + c.ClockUncertainty) / step))
	bin = max(0, min(63, bin))
	if mask&(uint64(1)<<bin) != 0 {
		return true
	}
	// A sample on a bin edge belongs to either adjacent closed interval.
	return bin > 0 && math.Abs(offset+c.ClockUncertainty-float64(bin)*step) < 1e-8 && mask&(uint64(1)<<(bin-1)) != 0
}

func curveRangePossible(t tracks.Track, p geo.Vec, radius float64, e acoustic.Event, c Config, observed []scoreObservation, from, to float64) bool {
	arrivalStart, arrivalEnd, valid := t.ArrivalSpan(p, c.SoundSpeed)
	if !valid {
		return false
	}
	possible := 0
	for _, o := range observed {
		if to+o.time >= arrivalStart-radius/c.SoundSpeed && from+o.time <= arrivalEnd+radius/c.SoundSpeed {
			possible++
		}
	}
	if float64(possible) < .95*float64(len(observed)) {
		return false
	}
	if len(e.Ridge) == 0 || len(observed) < 24 {
		return true
	}
	n, maxHz := 0, 0.0
	for _, o := range observed {
		if o.hasRidge {
			n++
			maxHz = math.Max(maxHz, o.hz)
		}
	}
	limit := math.Max(1.5, maxHz*.012)
	if !e.Partial && e.Measurement != nil && e.Measurement.Usable {
		limit = math.Min(limit, e.Measurement.LinearRMSE*.9)
	}
	type interval struct{ lo, hi, y float64 }
	var ranges []interval
	for _, q := range []int{0, 12, 6, 3, 9, 1, 11, 2, 10, 4, 8, 5, 7} {
		o := observed[q*(len(observed)-1)/12]
		if !o.hasRidge {
			continue
		}
		lo, hi, ok := t.DopplerBounds(p, radius, from+o.time, to+o.time, c.SoundSpeed, c.MaxGap)
		if !ok {
			continue
		} // unknown geometry must not reject a possible cell
		ranges = append(ranges, interval{lo, hi, o.hz})
	}
	lo, hi := e.FMin, e.FMax
	for k := 0; k < 32; k++ {
		f := (lo + hi) / 2
		derivative := 0.0
		for _, r := range ranges {
			if f*r.lo > r.y {
				derivative += r.lo * (f*r.lo - r.y)
			} else if f*r.hi < r.y {
				derivative += r.hi * (f*r.hi - r.y)
			}
		}
		if derivative > 0 {
			hi = f
		} else {
			lo = f
		}
	}
	f := (lo + hi) / 2
	error, scale := 0.0, 0.0
	for _, r := range ranges {
		residual := math.Max(0, math.Max(f*r.lo-r.y, r.y-f*r.hi))
		error += residual * residual
		scale += r.y * r.y
	}
	return error <= float64(n)*limit*limit+1e-7*math.Max(1, scale)
}
