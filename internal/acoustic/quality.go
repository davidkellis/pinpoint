package acoustic

import "math"

// These are diagnostic evidence tiers, NOT calibrated probabilities or aircraft
// classifications. A passing car can also Doppler-shift a stable source tone.
type CurveEvidence struct {
	Level             string   `json:"level"`
	Reasons           []string `json:"reasons"`
	Coverage          float64  `json:"ridge_coverage"`
	Contrast          float64  `json:"median_contrast_db"`
	HarmonicSupport   float64  `json:"harmonic_support"`
	Resolutions       int      `json:"resolution_count"`
	HeldoutRMSE       *float64 `json:"heldout_doppler_rmse_hz,omitempty"`
	HeldoutLinearRMSE *float64 `json:"heldout_linear_rmse_hz,omitempty"`
	Predictive        bool     `json:"heldout_pass"`
}

func assessCurve(s *Spectrum, e Event, points []ridgePoint) *CurveEvidence {
	q := &CurveEvidence{Level: "weak", Resolutions: 1, Reasons: []string{}}
	q.Coverage = float64(len(points)) / float64(points[len(points)-1].frame-points[0].frame+1)
	var contrast []float64
	harmonic, eligible := 0, 0
	for _, p := range e.Ridge {
		frame := int(math.Round((p.Time - s.Time(0)) / s.Step()))
		if v, ok := ridgeContrast(s, frame, p.Hz); ok {
			contrast = append(contrast, v)
		}
		// Evidence of a second tone following the same fractional frequency
		// changes. Missing/out-of-band harmonics are not negative evidence.
		available, supported := false, false
		for _, ratio := range []float64{.5, 2, 3} {
			if v, ok := ridgeContrast(s, frame, p.Hz*ratio); ok {
				available = true
				supported = supported || v >= 6
			}
		}
		if available {
			eligible++
			if supported {
				harmonic++
			}
		}
	}
	if len(contrast) > 0 {
		q.Contrast = median(contrast)
	}
	if eligible > 0 {
		q.HarmonicSupport = float64(harmonic) / float64(eligible)
	}
	if e.Measurement == nil || !e.Measurement.ShapePass {
		if e.Measurement != nil {
			q.Reasons = append(q.Reasons, e.Measurement.Reason)
		}
		return q
	}
	if q.Coverage < .85 {
		q.Reasons = append(q.Reasons, "Ridge covers less than 85% of the interval.")
	}
	if q.Contrast < 8 {
		q.Reasons = append(q.Reasons, "Tone is not consistently separated from nearby noise.")
	}
	if len(q.Reasons) > 0 {
		return q
	}
	// Hold out alternating time blocks, three times, retaining samples across
	// the whole pass so speed/frequency remain identifiable.
	// A half-window guard on either side of each boundary avoids shared FFT
	// samples between training and validation. No held-out points enter a fit.
	doppler, linear, ok := heldoutCurve(s, points)
	if ok {
		q.HeldoutRMSE = &doppler
		q.HeldoutLinearRMSE = &linear
	}
	// Test generalization against the competing pitch-trend model. An extra
	// fixed Hz cutoff incorrectly rejected clear real curves whose aircraft
	// maneuvered; the full-data fit already imposes a residual bound.
	q.Predictive = ok && doppler < linear*.7
	if !q.Predictive {
		q.Reasons = append(q.Reasons, "Doppler fit does not predict held-out audio substantially better than a pitch trend.")
	}
	finishEvidence(q)
	return q
}

func finishEvidence(q *CurveEvidence) {
	if !q.Predictive {
		q.Level = "weak"
		return
	}
	if q.Resolutions < 2 && q.HarmonicSupport < .65 {
		q.Level = "review"
		q.Reasons = []string{"Predictive curve, but needs corroboration in another spectrogram resolution or a matching harmonic."}
		return
	}
	q.Level = "strong"
	q.Reasons = []string{"Sustained, distinct tone; predictive Doppler shape; corroborated across resolutions or harmonics. Aircraft identity is unconfirmed."}
}

func ridgeContrast(s *Spectrum, frame int, hz float64) (float64, bool) {
	return ridgeContrastWidth(s, frame, hz, math.Max(12, hz*.025))
}
func ridgeContrastWidth(s *Spectrum, frame int, hz, width float64) (float64, bool) {
	if frame < 0 || frame >= len(s.Rows) {
		return 0, false
	}
	df := float64(s.SampleRate) / float64(s.Window)
	if hz-width < s.Hz(0) || hz+width > s.Hz(len(s.Rows[frame])-1) {
		return 0, false
	}
	peak := math.Inf(-1)
	for _, delta := range []float64{-df, 0, df} {
		if v, ok := s.Value(frame, hz+delta); ok {
			peak = math.Max(peak, v)
		}
	}
	var side []float64
	for _, sign := range []float64{-1, 1} {
		for k := 0; k < 5; k++ {
			v, _ := s.Value(frame, hz+sign*width*(.5+float64(k)/8))
			side = append(side, v)
		}
	}
	return peak - median(side), true
}

func idealHz(m RidgeMeasurement, t float64) float64 {
	// Closed-form retarded time relative to emission CPA; m.CPA is arrival CPA.
	c, v, d := 343.0, m.FitSpeed, m.FitSlant
	tr := t - m.CPA + d/c
	te := (c*c*tr - math.Sqrt(c*c*v*v*tr*tr+(c*c-v*v)*d*d)) / (c*c - v*v)
	return m.F0 * c / (c + v*v*te/math.Hypot(d, v*te))
}

func heldoutCurve(s *Spectrum, points []ridgePoint) (float64, float64, bool) {
	a, b := s.Time(points[0].frame), s.Time(points[len(points)-1].frame)
	guard := float64(s.Window) / float64(s.SampleRate) / 2
	var squared, linearSquared float64
	count := 0
	for fold := 0; fold < 3; fold++ {
		var train, test []ridgePoint
		blockWidth := (b - a) / 6
		for _, p := range points {
			t := s.Time(p.frame)
			block := min(5, int((t-a)/blockWidth))
			lo, hi := a+float64(block)*blockWidth, a+float64(block+1)*blockWidth
			if t < lo+guard || t > hi-guard {
				continue
			}
			if block%3 == fold {
				test = append(test, p)
			} else {
				train = append(train, p)
			}
		}
		if len(train) < 12 || len(test) < 5 {
			return 0, 0, false
		}
		fit := fitRidge(s, train)
		mt, mf := 0.0, 0.0
		for _, p := range train {
			mt += s.Time(p.frame) - a
			mf += p.hz
		}
		mt /= float64(len(train))
		mf /= float64(len(train))
		xy, xx := 0.0, 0.0
		for _, p := range train {
			dt := s.Time(p.frame) - a - mt
			xy += dt * (p.hz - mf)
			xx += dt * dt
		}
		for _, p := range test {
			t := s.Time(p.frame)
			delta := idealHz(fit, t) - p.hz
			squared += delta * delta
			delta = mf + xy/xx*(t-a-mt) - p.hz
			linearSquared += delta * delta
			count++
		}
	}
	return math.Sqrt(squared / float64(count)), math.Sqrt(linearSquared / float64(count)), count > 0
}

// SearchEvents is the automatic policy for both stationary and walking input.
// Weak fragments are never silently promoted because the recorder was moving.
func SearchEvents(events []Event, joint, includeWeak bool) []Event {
	var selected []Event
	for _, e := range events {
		if includeWeak && !joint || e.Evidence != nil && e.Evidence.Level == "strong" {
			selected = append(selected, e)
		}
	}
	if joint {
		return JointEvents(selected)
	}
	return selected
}

// CorroborateCurves checks the measured path directly in a second spectrogram.
// A fast sweep may be visible there yet fail ridge linking because the longer
// FFT smears its peak. This is a robustness check on the SAME recording, not
// independent evidence or a probability of aircraft identity.
func CorroborateCurves(report DetectionReport, other *Spectrum) DetectionReport {
	report.Events = append([]Event(nil), report.Events...)
	for i, e := range report.Events {
		if e.Evidence == nil || !e.Evidence.Predictive || e.Resolution == float64(other.Window)/float64(other.SampleRate) {
			continue
		}
		eligible, supported := 0, 0
		lastFrame := -1
		for k, p := range e.Ridge {
			if k == 0 || k == len(e.Ridge)-1 {
				continue
			}
			frame := int(math.Round((p.Time - other.Time(0)) / other.Step()))
			if frame == lastFrame {
				continue
			}
			lastFrame = frame
			before, after := e.Ridge[k-1], e.Ridge[k+1]
			rate := math.Abs((after.Hz - before.Hz) / (after.Time - before.Time))
			width := math.Max(math.Max(12, p.Hz*.025), rate*float64(other.Window)/float64(other.SampleRate)*1.5)
			contrast, ok := ridgeContrastWidth(other, frame, p.Hz, width)
			if !ok {
				continue
			}
			eligible++
			if contrast >= 8 {
				supported++
			}
		}
		if eligible >= 20 && float64(supported)/float64(eligible) >= .8 {
			q := *e.Evidence
			q.Resolutions = 2
			finishEvidence(&q)
			report.Events[i].Evidence = &q
		}
	}
	report.SetEvents(report.Events)
	return report
}

func evidenceRank(e Event) int {
	if e.Evidence == nil {
		return 0
	}
	switch e.Evidence.Level {
	case "strong":
		return 2
	case "review":
		return 1
	}
	return 0
}
