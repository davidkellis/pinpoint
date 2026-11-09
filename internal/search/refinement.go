package search

import (
	"math"
	"pinpoint/internal/acoustic"
	"pinpoint/internal/geo"
	"sort"
	"strings"
)

// followupSeeds balances flight combinations and keeps separated position/clock
// hypotheses within each. Each point still has the clock selected by the original
// search; this does not reconstruct discarded clocks or invent new locations.
func followupSeeds(candidates []Candidate, metres, seconds float64, limit int) []Candidate {
	if limit <= 0 {
		return nil
	}
	sorted := append([]Candidate(nil), candidates...)
	sortCandidates(sorted)
	groups := map[string][]Candidate{}
	var order []string
	for _, p := range sorted {
		key := flightKey(p)
		if _, ok := groups[key]; !ok {
			order = append(order, key)
		}
		groups[key] = append(groups[key], p)
	}
	// Reserve half the budget for alternative positions/clocks, rather than
	// filling it entirely with one point for every low-ranked flight group.
	order = order[:min(len(order), max(1, limit/2))]
	var out []Candidate
	chosen := map[string][]Candidate{}
	cursors := map[string]int{}
	for len(out) < limit {
		added := false
		for _, key := range order {
			for cursors[key] < len(groups[key]) {
				p := groups[key][cursors[key]]
				cursors[key]++
				duplicate := false
				for _, q := range chosen[key] {
					if math.Abs(p.ClockOffset-q.ClockOffset) < seconds && geo.ECEF(p.Lat, p.Lon, p.Height).Sub(geo.ECEF(q.Lat, q.Lon, q.Height)).Norm() < metres {
						duplicate = true
						break
					}
				}
				if duplicate {
					continue
				}
				out = append(out, p)
				chosen[key] = append(chosen[key], p)
				added = true
				break
			}
			if len(out) == limit {
				break
			}
		}
		if !added {
			break
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	return out
}

// Reserve seeds for competing flight combinations before filling with spatial
// peaks. A dense cluster from the first pair must not consume every refinement.
func refinementSeeds(sorted []Candidate, spacing float64, limit int) []Candidate {
	var seeds []Candidate
	groups := map[string]bool{}
	key := flightKey
	for _, p := range sorted {
		k := key(p)
		if !groups[k] {
			seeds = append(seeds, p)
			groups[k] = true
		}
		if len(seeds) == limit {
			return seeds
		}
	}
	for _, p := range sorted {
		far := true
		for _, q := range seeds {
			if geo.ECEF(p.Lat, p.Lon, p.Height).Sub(geo.ECEF(q.Lat, q.Lon, q.Height)).Norm() < 2*spacing {
				far = false
				break
			}
		}
		if far {
			seeds = append(seeds, p)
		}
		if len(seeds) == limit {
			break
		}
	}
	return seeds
}

func flightKey(p Candidate) string {
	ids := make([]string, len(p.Events))
	for i, e := range p.Events {
		ids[i] = e.Track
	}
	return strings.Join(ids, "\x00")
}
func refinementConfig(c Config, seed Candidate, radius float64) Config {
	c.Events = append([]acoustic.Event(nil), c.Events...)
	c.eventTrackIDs = make([]string, len(c.Events))
	speed := math.Inf(1)
	for i, m := range seed.Events {
		c.eventTrackIDs[i] = m.Track
		e := &c.Events[i]
		speed = math.Min(speed, math.Max(15, e.MinSpeed))
		if len(e.Ridge) == 0 {
			e.FMin = math.Max(e.FMin, m.F0-2*e.FStep)
			e.FMax = math.Min(e.FMax, m.F0+2*e.FStep)
			e.FStep = math.Min(e.FStep, .05)
		}
	}
	// Position changes can move CPA; preserve that local clock range, bounded
	// by the user's original absolute timing uncertainty. Every event still
	// uses the very same offset at each evaluation.
	span := 2*c.ClockStep + 2*radius/speed + 2*radius/c.SoundSpeed
	lo, hi := math.Max(-c.ClockUncertainty, seed.ClockOffset-span), math.Min(c.ClockUncertainty, seed.ClockOffset+span)
	step := math.Min(c.ClockStep, .1)
	c.clockOffsets = nil
	for offset := lo; offset < hi; offset += step {
		c.clockOffsets = append(c.clockOffsets, offset)
	}
	c.clockOffsets = append(c.clockOffsets, hi, seed.ClockOffset)
	return c
}
