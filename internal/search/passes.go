package search

import (
	"fmt"
	"math"
	"sort"

	"pinpoint/internal/acoustic"
	"pinpoint/internal/geo"
	"pinpoint/internal/tracks"
)

// PassProminence is how far an aircraft must move away from the receiver, and
// then come back, between two curves before they count as separate passes.
// A straight constant-velocity pass has a single closest approach, so it can
// never be split into two "agreeing" passes however its curve is cut up.
const PassProminence = 1000.

// passStep samples slant range along arrival time, in seconds.
const passStep = 2.

// owner is the key that one curve may claim in the distinct-explanation
// assignment: an aircraft, or one of its separate passes over the receiver.
func (q JointRidgeMatch) owner() string {
	if q.Pass == 0 {
		return q.Aircraft
	}
	return fmt.Sprintf("%s#%d", q.Aircraft, q.Pass)
}

// passBoundaries returns the arrival times (audio seconds) of far points
// between prominent closest approaches of one aircraft at a fixed receiver:
// local maxima of slant range from which the range falls by at least
// prominence on both sides, within continuous track coverage. Track gaps never
// create a boundary, so coverage gaps can only merge passes, never split them.
func passBoundaries(ts []tracks.Track, receiver geo.Vec, epoch, from, to, sound, maxGap, prominence float64) []float64 {
	var out []float64
	for _, t := range ts {
		var times, ranges []float64
		flush := func() {
			out = append(out, prominentMaxima(times, ranges, prominence)...)
			times, ranges = times[:0], ranges[:0]
		}
		for a := from; a <= to; a += passStep {
			_, d, ok := t.Received(receiver, epoch+a, sound, maxGap)
			if !ok {
				flush()
				continue
			}
			times, ranges = append(times, a), append(ranges, d)
		}
		flush()
	}
	sort.Float64s(out)
	return out
}

// prominentMaxima finds maxima of one continuous range series that are at
// least prominence above the lowest range on each side before the series
// returns to that height (or ends).
func prominentMaxima(times, ranges []float64, prominence float64) []float64 {
	var out []float64
	lowLeft := math.Inf(1)
	for i, d := range ranges {
		lowLeft = math.Min(lowLeft, d)
		if i == 0 || i == len(ranges)-1 || d < ranges[i-1] || d < ranges[i+1] || d-lowLeft < prominence {
			continue
		}
		lowRight := d
		for _, r := range ranges[i+1:] {
			if r > d {
				break
			}
			lowRight = math.Min(lowRight, r)
		}
		if d-lowRight >= prominence {
			out = append(out, times[i])
			lowLeft = d // the next pass's descent starts here
		}
	}
	return out
}

// assignPasses numbers the passes of every aircraft that explains more than
// one curve at this receiver and clock, so that clearly separate passes of one
// aircraft (out and back again, for example a circling or turning aircraft)
// may explain different curves. Curves in the same pass keep one owner.
func assignPasses(rows [][]JointRidgeMatch, h CurveReceiver, epoch float64, ts []tracks.Track, events []acoustic.Event) {
	if h.East != 0 || h.North != 0 || h.Up != 0 {
		return // separate passes are defined for a fixed receiver only
	}
	curves := map[string]map[int]bool{}
	for i, row := range rows {
		for _, q := range row {
			if curves[q.Aircraft] == nil {
				curves[q.Aircraft] = map[int]bool{}
			}
			curves[q.Aircraft][i] = true
		}
	}
	receiver := geo.ECEF(h.Lat, h.Lon, h.Height)
	for aircraft, set := range curves {
		if len(set) < 2 {
			continue
		}
		from, to := math.Inf(1), math.Inf(-1)
		for i := range set {
			from, to = math.Min(from, events[i].Start), math.Max(to, events[i].End)
		}
		var own []tracks.Track
		for _, t := range ts {
			if guidedAircraftID(t.ID) == aircraft {
				own = append(own, t)
			}
		}
		bounds := passBoundaries(own, receiver, epoch, from, to, h.SoundSpeed, h.MaxGap, PassProminence)
		if len(bounds) == 0 {
			continue
		}
		for i := range set {
			mid := (events[i].Start + events[i].End) / 2
			pass := sort.SearchFloat64s(bounds, mid)
			for k := range rows[i] {
				if rows[i][k].Aircraft == aircraft {
					rows[i][k].Pass = pass
				}
			}
		}
	}
}
