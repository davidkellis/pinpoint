package acoustic

import (
	"context"
	"fmt"
	"math"
)

type TraceDecision struct {
	Window     int    `json:"window"`
	Candidates int    `json:"eligible_curves"`
	Polished   bool   `json:"sub_bin_trace_used"`
	Reason     string `json:"reason"`
	Event      *Event `json:"selected_event,omitempty"`
}
type TraceReport struct {
	Note      string          `json:"note"`
	Decisions []TraceDecision `json:"decisions"`
	Events    []Event         `json:"events"`
}

// RefineMeasuredCurve refines a supplied strong audio ridge using local measured
// spectral maxima and sub-bin interpolation. No flight geometry enters the
// corridor. Failed refinement returns the original curve and false; it never
// promotes a weak proposal or interpolates missing evidence into observations.
func RefineMeasuredCurve(s, other *Spectrum, e Event) (Event, bool) {
	if s == nil || len(s.Rows) == 0 || len(s.Rows[0]) < 3 || s.SampleRate <= 0 || s.Window <= 0 || s.Hop <= 0 || len(e.Ridge) < 8 || e.Evidence == nil || e.Evidence.Level != "strong" || e.Measurement == nil || !finite(e.Start) || !finite(e.End) || e.End <= e.Start {
		return e, false
	}
	return polishMeasuredRidge(s, other, e)
}

// TraceWindows binds reviewed time/source-frequency windows to independently
// discovered acoustic ridges. There is no receiver location or flight argument.
// Every window must contain one distinct strong curve; ambiguity is an error,
// never resolved by asking which choice produces the desired location.
func TraceWindows(ctx context.Context, primary, secondary *Spectrum, d DetectionReport, windows []Event) (TraceReport, error) {
	r := TraceReport{Note: "Reviewed time/source-frequency windows select independently detected strong acoustic curves. Measured peaks are refined locally and checked again; failed refinement retains the original measured trace. This is not automatic aircraft classification or blind event selection. No ADS-B, recording location or flight IDs enter tracing.", Decisions: []TraceDecision{}, Events: []Event{}}
	if len(windows) < 2 {
		return r, fmt.Errorf("joint tracing needs at least two reviewed sound windows")
	}
	used := map[int]bool{}
	failed := false
	for i, w := range windows {
		if err := ctx.Err(); err != nil {
			return r, err
		}
		decision := TraceDecision{Window: i + 1}
		if !finite(w.Start) || !finite(w.End) || w.End <= w.Start || !finite(w.FMin) || !finite(w.FMax) || w.FMin <= 0 || w.FMax < w.FMin || i > 0 && w.Start < windows[i-1].End {
			return r, fmt.Errorf("reviewed windows must have valid frequency bounds and ordered, nonoverlapping times")
		}
		var indices []int
		for j, e := range d.Events {
			if e.Evidence == nil || e.Evidence.Level != "strong" || !e.Evidence.Predictive || e.Measurement == nil || !e.Measurement.ShapePass || len(e.Ridge) < 8 {
				continue
			}
			overlap := math.Min(w.End, e.End) - math.Max(w.Start, e.Start)
			if overlap < .6*(w.End-w.Start) || overlap < .6*(e.End-e.Start) || e.Measurement.F0 < w.FMin || e.Measurement.F0 > w.FMax {
				continue
			}
			// Do not trim a strong trace and inherit confidence for the fragment.
			// The reviewed window may be slightly narrower than the measured extent.
			indices = append(indices, j)
		}
		decision.Candidates = len(indices)
		if len(indices) != 1 {
			decision.Reason = "Need exactly one strong curve in this window/source-frequency band; review or narrow the window."
			r.Decisions = append(r.Decisions, decision)
			failed = true
			continue
		}
		j := indices[0]
		if used[j] {
			decision.Reason = "This measured curve was already assigned to another window."
			r.Decisions = append(r.Decisions, decision)
			failed = true
			continue
		}
		used[j] = true
		e := d.Events[j]
		s, other := primary, secondary
		if secondary != nil && math.Abs(e.Resolution-float64(secondary.Window)/float64(secondary.SampleRate)) < 1e-6 {
			s, other = secondary, primary
		}
		polished, ok := polishMeasuredRidge(s, other, e)
		if ok {
			e = polished
			decision.Polished = true
			decision.Reason = "Measured local peaks refined below bin spacing; acoustic checks passed again."
		} else {
			decision.Reason = "Retained original strong measured trace; local refinement did not pass all checks."
		}
		// Preserve the user's physical screening limits. An ideal Doppler fit
		// is not a reliable hard velocity/slant bound for a turning aircraft.
		e.FMin, e.FMax, e.FStep = w.FMin, w.FMax, w.FStep
		e.MinSpeed, e.MaxSpeed, e.MinSlant, e.MaxSlant = w.MinSpeed, w.MaxSpeed, w.MinSlant, w.MaxSlant
		e.Harmonics = []int{1} // measured ridge is the tracked tone, regardless of harmonic order
		e.Partial = !e.Measurement.Usable
		e.CPATime = (e.Start + e.End) / 2
		e.CPATolerance = 0
		if !e.Partial {
			e.CPATime = e.Measurement.CPA
			e.CPATolerance = e.Measurement.CPATolerance
		}
		decision.Event = &e
		r.Events = append(r.Events, e)
		r.Decisions = append(r.Decisions, decision)
	}
	if failed {
		return r, fmt.Errorf("not every reviewed window has a unique strong measured curve; inspect curve-traces.json")
	}
	for i := 1; i < len(r.Events); i++ {
		if r.Events[i].Start < r.Events[i-1].End {
			return r, fmt.Errorf("selected measured curves overlap; review distinct flyovers")
		}
	}
	return r, nil
}

// The fitted Doppler function does not set any new frequency. A short corridor
// interpolates the already measured ridge; each retained observation must be a
// local spectral maximum. Unsupported gaps remain absent.
func polishMeasuredRidge(s, other *Spectrum, e Event) (Event, bool) {
	df := float64(s.SampleRate) / float64(s.Window)
	first := max(0, int(math.Ceil((e.Start-s.Time(0))/s.Step())))
	last := min(len(s.Rows)-1, int(math.Floor((e.End-s.Time(0))/s.Step())))
	var points []ridgePoint
	for frame := first; frame <= last; frame++ {
		hz, ok := e.RidgeHz(s.Time(frame))
		if !ok {
			continue
		}
		bin := int(math.Round(hz/df)) - s.FirstBin
		best := -1
		distance := math.Inf(1)
		for k := max(1, bin-2); k <= min(len(s.Rows[frame])-2, bin+2); k++ {
			row := s.Rows[frame]
			if row[k] < 6 || row[k] <= row[k-1] || row[k] < row[k+1] {
				continue
			}
			f := refinedPeakHz(s, frame, k)
			if math.Abs(f-hz) > 2*df {
				continue
			}
			contrast, valid := ridgeContrast(s, frame, f)
			if !valid || contrast < 6 {
				continue
			}
			if delta := math.Abs(f - hz); delta < distance {
				best, distance = k, delta
			}
		}
		if best >= 0 {
			points = append(points, ridgePoint{frame, refinedPeakHz(s, frame, best), float64(s.Rows[frame][best])})
		}
	}
	if len(points) < 20 || float64(len(points))/float64(last-first+1) < .85 {
		return e, false
	}
	m := fitRidge(s, points)
	if !m.ShapePass || e.Measurement.Usable && !m.Usable {
		return e, false
	}
	out := eventForRidge(s, points, m)
	if other != nil {
		d := CorroborateCurves(DetectionReport{Events: []Event{out}}, other)
		out = d.Events[0]
	}
	if out.Evidence.Level != "strong" || out.End-out.Start < .95*(e.End-e.Start) {
		return e, false
	}
	return out, true
}

func refinedPeakHz(s *Spectrum, frame, bin int) float64 {
	row := s.Rows[frame]
	a, b, c := float64(row[bin-1]), float64(row[bin]), float64(row[bin+1])
	curvature := a - 2*b + c
	delta := 0.
	if curvature < 0 {
		delta = math.Max(-.5, math.Min(.5, .5*(a-c)/curvature))
	}
	return s.Hz(bin) + delta*float64(s.SampleRate)/float64(s.Window)
}
