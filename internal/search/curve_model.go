package search

import (
	"context"
	"fmt"
	"math"
	"time"

	"pinpoint/internal/acoustic"
	"pinpoint/internal/geo"
	"pinpoint/internal/tracks"
)

// CurveReceiver describes ONE supplied receiver path and clock for all curves.
// Velocity is local east/north/up, in still air. This short-window linear path
// is an experimental approximation, not a recovered walking route.
type CurveReceiver struct {
	ReceiverHypothesis
	East  float64 `json:"east_mps"`
	North float64 `json:"north_mps"`
	Up    float64 `json:"up_mps"`
}

type CurveModelOptions struct {
	Window          float64 `json:"fft_window_s"`
	MaxSourceChange float64 `json:"max_fractional_source_change"`
	OmitCurve       bool    `json:"omit_prediction_samples,omitempty"`
}

type CurveModelFit struct {
	Model         string                 `json:"model"`
	Source        float64                `json:"source_hz"`
	Slope         float64                `json:"source_slope_hz_per_emission_s"`
	Reference     float64                `json:"reference_emission_audio_s"`
	Change        float64                `json:"fractional_source_change"`
	AtBound       bool                   `json:"source_change_at_bound"`
	Training      float64                `json:"training_rmse_hz"`
	TrainConstant float64                `json:"training_constant_rmse_hz"`
	TrainLinear   float64                `json:"training_linear_rmse_hz"`
	Reserved      float64                `json:"reserved_rmse_hz"`
	Constant      float64                `json:"reserved_constant_rmse_hz"`
	Linear        float64                `json:"reserved_linear_rmse_hz"`
	Criterion     float64                `json:"training_selection_criterion"`
	Coverage      float64                `json:"geometry_coverage"`
	TrainCount    int                    `json:"training_samples"`
	TestCount     int                    `json:"reserved_samples"`
	Shape         bool                   `json:"descriptive_shape_agreement"`
	Selected      bool                   `json:"selected_on_training"`
	Curve         []acoustic.RidgeSample `json:"predicted_curve"`
}

type CurveModelReview struct {
	Receiver CurveReceiver     `json:"supplied_receiver_path"`
	Track    string            `json:"track_id"`
	Options  CurveModelOptions `json:"options"`
	Fold     FoldPlan          `json:"fold_plan"`
	Fits     []CurveModelFit   `json:"fits"`
	Status   string            `json:"status"`
	Note     string            `json:"note"`
}

type curveModelPoint struct {
	time, emitted, factor, hz float64
	test                      bool
}

// FitRidgeCurve tests source pitch at EMISSION time, including the receiver's
// position and velocity at ARRIVAL. Constant and bounded linear source-pitch
// models compete on training blocks using a complexity penalty. Reserved
// ridge measurements never choose the model, source pitch, motion or clock.
// The ridge itself can already have been selected using the whole recording;
// its reserved residual is therefore descriptive, not an unbiased detector
// validation or an independently verified aircraft/location identification.
func FitRidgeCurve(ctx context.Context, h CurveReceiver, t tracks.Track, e acoustic.Event, o CurveModelOptions) (CurveModelReview, error) {
	r := CurveModelReview{Receiver: h, Track: t.ID, Options: o, Fits: []CurveModelFit{}, Status: "unavailable", Note: "Experimental forward-model study of an existing ridge. Receiver path and one clock are supplied; source variation is bounded and evaluated at emission time. Models are selected on training blocks only. Whole-recording ridge discovery can already use reserved audio. Shape agreement is not independent aircraft identity, spectral confirmation or blind GPS validation. More flexible source/motion models can increase geographic ambiguity."}
	for _, x := range []float64{h.Lat, h.Lon, h.Height, h.Clock, h.SoundSpeed, h.MaxGap, h.East, h.North, h.Up, o.Window, o.MaxSourceChange, e.Start, e.End, e.Resolution} {
		if !geo.Finite(x) {
			return r, fmt.Errorf("nonfinite curve model setting")
		}
	}
	if math.Abs(h.Lat) > 85 || math.Abs(h.Lon) > 180 || h.Height < -1000 || h.Height > 10000 || math.Abs(h.Clock) > 3600 || h.SoundSpeed < 300 || h.SoundSpeed > 370 || h.MaxGap <= 0 || h.MaxGap > 120 || math.Sqrt(h.East*h.East+h.North*h.North+h.Up*h.Up) > 15 || o.Window <= 0 || o.Window > 16 || o.MaxSourceChange < 0 || o.MaxSourceChange > .25 || e.End <= e.Start || e.Resolution < 0 || e.Resolution > 16 {
		return r, fmt.Errorf("invalid curve model limits")
	}
	start, err := time.Parse(time.RFC3339Nano, h.RecordingStart)
	if err != nil {
		return r, fmt.Errorf("recording_start requires a time zone: %w", err)
	}
	epoch := float64(start.Unix()) + float64(start.Nanosecond())/1e9 + h.Clock
	meta := &acoustic.Spectrum{SampleRate: 1024, Window: int(o.Window * 1024)}
	r.Fold = eventFold(e, meta)
	origin := geo.ECEF(h.Lat, h.Lon, h.Height)
	velocity := geo.ENUVector(h.Lat, h.Lon, h.East, h.North, h.Up)
	var points []curveModelPoint
	expected, ntrain, ntest := 0, 0, 0
	lo, hi := math.Inf(1), math.Inf(-1)
	lastTime := math.Inf(-1)
	for _, q := range e.Ridge {
		if err := ctx.Err(); err != nil {
			return r, err
		}
		if !geo.Finite(q.Time) || !geo.Finite(q.Hz) || q.Hz <= 0 || q.Time <= lastTime {
			return r, fmt.Errorf("ridge samples must be finite, positive and strictly ordered")
		}
		lastTime = q.Time
		train, test := r.Fold.Contains(q.Time, "train"), r.Fold.Contains(q.Time, "validation")
		if !train && !test {
			continue
		}
		expected++
		p := origin.Add(velocity.Scale(q.Time))
		factor, _, emitted, ok := t.ReceivedMoving(p, velocity, epoch+q.Time, h.SoundSpeed, h.MaxGap)
		if !ok {
			continue
		}
		emitted -= epoch
		lo = math.Min(lo, emitted)
		hi = math.Max(hi, emitted)
		points = append(points, curveModelPoint{q.Time, emitted, factor, q.Hz, test})
		if test {
			ntest++
		} else {
			ntrain++
		}
	}
	if ntrain < 8 || ntest < 8 || float64(len(points)) < .95*float64(expected) || hi <= lo {
		return r, nil
	}
	ref, span := (lo+hi)/2, hi-lo
	var aa, ab, bb, ay, by, sy, st, stt, sty float64
	for _, q := range points {
		if q.test {
			continue
		}
		a, b := q.factor, q.factor*(q.emitted-ref)/span
		aa += a * a
		ab += a * b
		bb += b * b
		ay += a * q.hz
		by += b * q.hz
		sy += q.hz
		st += q.time
		stt += q.time * q.time
		sty += q.time * q.hz
	}
	if aa <= 0 {
		return r, nil
	}
	constant := ay / aa
	n := float64(ntrain)
	flat := sy / n
	den := n*stt - st*st
	if den <= 0 {
		return r, nil
	}
	lineSlope := (n*sty - st*sy) / den
	lineIntercept := (sy - lineSlope*st) / n
	type parameters struct {
		name                string
		intercept, gradient float64
		count               int
		bound               bool
	}
	models := []parameters{{"constant_source", constant, 0, 1, false}}
	if o.MaxSourceChange > 0 {
		determinant := aa*bb - ab*ab
		if determinant > 1e-12*aa*bb {
			f, b := (ay*bb-by*ab)/determinant, (by*aa-ay*ab)/determinant
			bound := false
			if f <= 0 || math.Abs(b/f) > o.MaxSourceChange {
				best := math.Inf(1)
				for _, fraction := range []float64{-o.MaxSourceChange, o.MaxSourceChange} {
					y := (ay + fraction*by) / (aa + 2*fraction*ab + fraction*fraction*bb)
					error := 0.
					for _, q := range points {
						if !q.test {
							d := q.factor*y*(1+fraction*(q.emitted-ref)/span) - q.hz
							error += d * d
						}
					}
					if error < best {
						best = error
						f = y
						b = y * fraction
					}
				}
				bound = true
			}
			models = append(models, parameters{"linear_source", f, b, 2, bound})
		}
	}
	for _, m := range models {
		fit := CurveModelFit{Model: m.name, Source: m.intercept, Slope: m.gradient / span, Reference: ref, Change: m.gradient / m.intercept, AtBound: m.bound, Coverage: float64(len(points)) / float64(expected), TrainCount: ntrain, TestCount: ntest, Curve: []acoustic.RidgeSample{}}
		trainSquared, testSquared, flatSquared, lineSquared, mean := 0., 0., 0., 0., 0.
		trainFlat, trainLine := 0., 0.
		for _, q := range points {
			predicted := q.factor * (fit.Source + fit.Slope*(q.emitted-ref))
			d := predicted - q.hz
			if q.test {
				testSquared += d * d
				flatSquared += math.Pow(flat-q.hz, 2)
				lineSquared += math.Pow(lineIntercept+lineSlope*q.time-q.hz, 2)
				mean += q.hz
			} else {
				trainSquared += d * d
				trainFlat += math.Pow(flat-q.hz, 2)
				trainLine += math.Pow(lineIntercept+lineSlope*q.time-q.hz, 2)
			}
		}
		fit.Training = math.Sqrt(trainSquared / n)
		fit.TrainConstant = math.Sqrt(trainFlat / n)
		fit.TrainLinear = math.Sqrt(trainLine / n)
		fit.Reserved = math.Sqrt(testSquared / float64(ntest))
		fit.Constant = math.Sqrt(flatSquared / float64(ntest))
		fit.Linear = math.Sqrt(lineSquared / float64(ntest))
		// A floor tied to FFT bin quantization prevents near-zero residuals
		// from rewarding an extra parameter for numerically tiny improvements.
		noiseFloor := 1 / (12 * o.Window * o.Window)
		fit.Criterion = n*math.Log(math.Max(trainSquared/n, noiseFloor)) + float64(m.count)*math.Log(n)
		limit := math.Max(1.5, .012*mean/float64(ntest))
		fit.Shape = fit.Training <= limit && fit.Reserved <= limit && fit.Reserved < .9*math.Min(fit.Constant, fit.Linear)
		if !o.OmitCurve {
			for _, q := range e.Ridge {
				p := origin.Add(velocity.Scale(q.Time))
				factor, _, emitted, ok := t.ReceivedMoving(p, velocity, epoch+q.Time, h.SoundSpeed, h.MaxGap)
				if ok {
					fit.Curve = append(fit.Curve, acoustic.RidgeSample{Time: q.Time, Hz: factor * (fit.Source + fit.Slope*(emitted-epoch-ref))})
				}
			}
		}
		r.Fits = append(r.Fits, fit)
	}
	best := 0
	for i := range r.Fits {
		if r.Fits[i].Criterion < r.Fits[best].Criterion {
			best = i
		}
	}
	r.Fits[best].Selected = true
	r.Status = "modeled"
	return r, nil
}
