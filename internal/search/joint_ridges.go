package search

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"

	"pinpoint/internal/acoustic"
	"pinpoint/internal/geo"
	"pinpoint/internal/tracks"
)

type JointRidgeMatch struct {
	Event    int    `json:"event"`
	Track    string `json:"track_id"`
	Aircraft string `json:"aircraft_identity"`
	// Pass numbers clearly separate passes of one aircraft over the receiver
	// (see assignPasses); zero unless separate passes are enabled.
	Pass               int           `json:"pass,omitempty"`
	Fit                CurveModelFit `json:"fit"`
	NormalizedTraining float64       `json:"normalized_training_rmse"`
}
type JointRidgeReview struct {
	Matches       []JointRidgeMatch `json:"training_selected_matches"`
	Valid         bool              `json:"all_curves_assigned"`
	ReservedPass  bool              `json:"all_reserved_ridge_checks_pass"`
	WorstTraining float64           `json:"worst_normalized_training_rmse"`
	Note          string            `json:"note"`
}

// assignRidgeAircraft minimizes the worst training residual, with one distinct
// aircraft per observed curve. Thresholded bipartite matching avoids a greedy
// assignment that can discard a valid combination. Reserved errors never pick
// an aircraft or rescue a failing training winner.
func assignRidgeAircraft(rows [][]JointRidgeMatch) ([]JointRidgeMatch, bool) {
	return assignRidgeAircraftBy(rows, func(q JointRidgeMatch) float64 { return q.NormalizedTraining })
}

// assignRidgeAircraftBy is assignRidgeAircraft with an explicit per-match cost.
func assignRidgeAircraftBy(rows [][]JointRidgeMatch, cost func(JointRidgeMatch) float64) ([]JointRidgeMatch, bool) {
	if len(rows) == 0 {
		return nil, false
	}
	var thresholds []float64
	for i := range rows {
		if len(rows[i]) == 0 {
			return nil, false
		}
		sort.SliceStable(rows[i], func(a, b int) bool {
			if ca, cb := cost(rows[i][a]), cost(rows[i][b]); ca != cb {
				return ca < cb
			}
			return rows[i][a].Track < rows[i][b].Track
		})
		for _, q := range rows[i] {
			thresholds = append(thresholds, cost(q))
		}
	}
	sort.Float64s(thresholds)
	match := func(limit float64) ([]JointRidgeMatch, bool) {
		owners := map[string]int{}
		choices := make([]int, len(rows))
		var augment func(int, map[string]bool) bool
		augment = func(event int, seen map[string]bool) bool {
			for j, q := range rows[event] {
				if cost(q) > limit {
					break
				}
				key := q.owner()
				if seen[key] {
					continue
				}
				seen[key] = true
				old, used := owners[key]
				if !used || augment(old, seen) {
					owners[key] = event
					choices[event] = j
					return true
				}
			}
			return false
		}
		for i := range rows {
			if !augment(i, map[string]bool{}) {
				return nil, false
			}
		}
		selected := make([]JointRidgeMatch, len(rows))
		for i := range rows {
			selected[i] = rows[i][choices[i]]
		}
		return selected, true
	}
	i := sort.Search(len(thresholds), func(i int) bool { _, ok := match(thresholds[i]); return ok })
	if i == len(thresholds) {
		return nil, false
	}
	return match(thresholds[i])
}

// FitJointRidges tests complete audio-selected curves at ONE common receiver
// and clock. No curve is cropped around a candidate CPA; no event gets its own
// time correction. This is a conditional measured-ridge fit, not acoustic
// detection, calibrated confidence, silence evidence or an independent GPS fix.
func FitJointRidges(ctx context.Context, h CurveReceiver, ts []tracks.Track, events []acoustic.Event, o CurveModelOptions) (JointRidgeReview, error) {
	r := JointRidgeReview{Matches: []JointRidgeMatch{}, Note: "Complete observed curves, one receiver/path/clock, distinct aircraft. Training residuals select assignment and source parameters; reserved ridge errors never rescue another assignment. Audio-selected ridges can already use the whole recording. This is not spectral confirmation or calibrated aircraft/GPS confidence."}
	if len(events) < 1 || len(events) > 8 {
		return r, fmt.Errorf("joint ridge fitting requires 1..8 observed curves")
	}
	for i, e := range events {
		if len(e.Ridge) < 8 || i > 0 && e.Start < events[i-1].End {
			return r, fmt.Errorf("observed curves need ordered, nonoverlapping windows and measured ridge samples")
		}
	}
	start, err := time.Parse(time.RFC3339Nano, h.RecordingStart)
	if err != nil {
		return r, err
	}
	rows, err := ridgeRows(ctx, h, start, ts, events, o, math.Inf(1))
	if err != nil {
		return r, err
	}
	selected, ok := assignRidgeAircraft(rows)
	if !ok {
		return r, nil
	}
	r.Valid = true
	r.ReservedPass = true
	r.Matches = selected
	for _, m := range selected {
		r.WorstTraining = math.Max(r.WorstTraining, m.NormalizedTraining)
		r.ReservedPass = r.ReservedPass && m.Fit.Shape
	}
	return r, nil
}

// ridgeRows fits every observed curve against every aircraft in acoustic range
// at one receiver and clock. Each row lists the eligible aircraft explanations
// for one curve, keeping only fits whose normalized training residual is at most
// maxNormalized. Event numbers are 1-based positions in events.
func ridgeRows(ctx context.Context, h CurveReceiver, start time.Time, ts []tracks.Track, events []acoustic.Event, o CurveModelOptions, maxNormalized float64) ([][]JointRidgeMatch, error) {
	epoch := float64(start.Unix()) + float64(start.Nanosecond())/1e9 + h.Clock
	point := geo.ECEF(h.Lat, h.Lon, h.Height)
	rows := make([][]JointRidgeMatch, len(events))
	for i, e := range events {
		mean := 0.
		for _, p := range e.Ridge {
			mean += p.Hz
		}
		mean /= float64(len(e.Ridge))
		scale := math.Max(1.5, mean*.012)
		for _, t := range ts {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			// Conservative range screening; the complete curve still needs full
			// retarded-time coverage and an acceptable reserved residual.
			if !t.WithinRange(point, epoch+e.Start, epoch+e.End, 15000+math.Hypot(h.East, h.North)*e.End, h.SoundSpeed, h.MaxGap) {
				continue
			}
			opts := o
			opts.OmitCurve = true // repeated location fits need statistics, not plot samples
			if e.Resolution > 0 {
				opts.Window = math.Max(o.Window, e.Resolution)
			}
			fit, err := FitRidgeCurve(ctx, h, t, e, opts)
			if err != nil {
				return nil, err
			}
			for _, f := range fit.Fits {
				if !f.Selected || f.Coverage < .999999 || f.Training/scale > maxNormalized {
					continue
				}
				f.Curve = nil
				rows[i] = append(rows[i], JointRidgeMatch{Event: i + 1, Track: t.ID, Aircraft: guidedAircraftID(t.ID), Fit: f, NormalizedTraining: f.Training / scale})
			}
		}
	}
	return rows, nil
}
