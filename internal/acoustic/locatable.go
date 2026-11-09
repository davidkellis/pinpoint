package acoustic

import (
	"math"
	"sort"
)

// DefaultLocatableMaxHz bounds the median pitch of curves offered to location
// search. Every labeled aircraft curve in the public regression data (Pathak
// propellers and jets, the AeroSonicDB A320, the YouTube reference) has a median
// below 1.3 kHz, and atmospheric absorption removes most tonal energy above a
// few kHz over kilometre-scale slant ranges (ISO 9613-1). Higher, short sweeps
// in phone recordings were not explained by any aircraft in diagnostics.
const DefaultLocatableMaxHz = 2500.

// LocatableCurve is one detector proposal offered to location search.
type LocatableCurve struct {
	Index    int     `json:"detector_index"` // 1-based position in the detection report
	Start    float64 `json:"start_s"`
	End      float64 `json:"end_s"`
	MedianHz float64 `json:"median_hz"`
	Samples  int     `json:"ridge_samples"`
}

func medianHz(r []RidgeSample) float64 {
	v := make([]float64, len(r))
	for i, p := range r {
		v[i] = p.Hz
	}
	sort.Float64s(v)
	if len(v)%2 == 1 {
		return v[len(v)/2]
	}
	return (v[len(v)/2-1] + v[len(v)/2]) / 2
}

// LocatableCurves selects every strong, measured detector proposal whose median
// pitch is at most maxHz, instead of betting on a single "best" curve: whether a
// sound is an aircraft with telemetry is decided later, by requiring several
// curves to agree on one location and clock. When more than limit qualify, the
// longest are kept. Results are ordered by start time.
func LocatableCurves(events []Event, maxHz float64, limit int) ([]Event, []LocatableCurve) {
	var picked []LocatableCurve
	for i, e := range events {
		if e.Evidence == nil || e.Evidence.Level != "strong" || len(e.Ridge) < 8 || e.End <= e.Start {
			continue
		}
		m := medianHz(e.Ridge)
		if !(m > 0) || m > maxHz || math.IsNaN(m) {
			continue
		}
		picked = append(picked, LocatableCurve{Index: i + 1, Start: e.Start, End: e.End, MedianHz: m, Samples: len(e.Ridge)})
	}
	if limit > 0 && len(picked) > limit {
		sort.SliceStable(picked, func(a, b int) bool {
			da, db := picked[a].End-picked[a].Start, picked[b].End-picked[b].Start
			if da != db {
				return da > db
			}
			return picked[a].Index < picked[b].Index
		})
		picked = picked[:limit]
	}
	sort.SliceStable(picked, func(a, b int) bool {
		if picked[a].Start != picked[b].Start {
			return picked[a].Start < picked[b].Start
		}
		return picked[a].Index < picked[b].Index
	})
	out := make([]Event, len(picked))
	for k, p := range picked {
		out[k] = events[p.Index-1]
	}
	return out, picked
}
