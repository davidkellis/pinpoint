package search

import (
	"fmt"
	"math"
	"pinpoint/internal/acoustic"
	"pinpoint/internal/geo"
	"pinpoint/internal/tracks"
	"sort"
)

// FitDiagnostic explains the unchanged scorer's gates; it never relaxes them.
type FitDiagnostic struct {
	Track           string           `json:"track_id"`
	Event           int              `json:"event"`
	Reason          string           `json:"scorer_status"`
	Rejections      []string         `json:"rejections"`
	ExpectedSamples int              `json:"expected_samples"`
	GeometrySamples int              `json:"geometry_samples"`
	RidgeSamples    int              `json:"ridge_samples"`
	F0              float64          `json:"f0_hz"`
	RMSE            *float64         `json:"ridge_rmse_hz,omitempty"`
	RMSELimit       *float64         `json:"ridge_rmse_limit_hz,omitempty"`
	Score           *float64         `json:"score_db,omitempty"`
	CPA             *tracks.Approach `json:"cpa,omitempty"`
	Points          []FitResidual    `json:"points"`
	FullResolution  *FitSupport      `json:"full_resolution,omitempty"`
}

// FitSupport describes a fixed hypothesis at every spectrogram frame. These
// are diagnostics, not independent validation or calibrated probabilities:
// position and clock may already have been chosen using this recording.
type FitSupport struct {
	Frames                   int      `json:"modeled_frames"`
	RidgeSamples             int      `json:"ridge_samples"`
	RMSE                     *float64 `json:"ridge_rmse_hz,omitempty"`
	MedianAbsResidual        *float64 `json:"median_absolute_residual_hz,omitempty"`
	MedianContrast           *float64 `json:"median_model_sideband_contrast_db,omitempty"`
	ContrastFrames           int      `json:"contrast_frames"`
	PositiveContrastFraction *float64 `json:"positive_contrast_fraction,omitempty"`
}

type FitResidual struct {
	Time          float64 `json:"time_s"`
	Observed      float64 `json:"observed_hz"`
	Predicted     float64 `json:"predicted_hz"`
	Residual      float64 `json:"residual_hz"`
	ImpliedSource float64 `json:"implied_source_hz"`
}

type DiagnosticReport struct {
	Lat    float64         `json:"lat"`
	Lon    float64         `json:"lon"`
	Height float64         `json:"height_ellipsoid_m"`
	Clock  float64         `json:"clock_offset_s"`
	Note   string          `json:"note"`
	Fits   []FitDiagnostic `json:"fits"`
}

// Diagnose evaluates a disclosed hypothesis, not a geolocation result. It also
// computes residuals when CPA/geometry gates fail, so failures remain inspectable.
func Diagnose(c Config, s *acoustic.Spectrum, ts []tracks.Track, lat, lon, offset float64) (DiagnosticReport, error) {
	r := DiagnosticReport{Lat: lat, Lon: lon, Clock: offset, Note: "Per-event diagnostic at a supplied hypothesis, not independent location evidence or a joint/silence search. Scoring gates are unchanged. Full-resolution checks hold position, clock and source frequency fixed; they do not independently validate a hypothesis selected on the same audio. Implied source frequency is descriptive; no variable-pitch model was fitted.", Fits: []FitDiagnostic{}}
	epoch, err := c.Validate(s)
	if err != nil {
		return r, err
	}
	if !geo.Finite(lat) || !geo.Finite(lon) || math.Abs(lat) > 90 || math.Abs(lon) > 180 || !geo.Finite(offset) || math.Abs(offset) > c.ClockUncertainty {
		return r, fmt.Errorf("invalid diagnostic coordinate or clock outside configured uncertainty")
	}
	if c.ObserverHeight != nil {
		r.Height = *c.ObserverHeight
	} else {
		terrain := geo.Terrain{Dir: c.TerrainDir}
		ground, e := terrain.Height(lat, lon)
		if e != nil {
			return r, e
		}
		und := 0.0
		if c.GeoidGrid != "" {
			g, e := geo.LoadGeoid(c.GeoidGrid)
			if e != nil {
				return r, e
			}
			und, e = g.Height(lat, lon)
			if e != nil {
				return r, e
			}
		} else {
			und = *c.GeoidOffset
		}
		r.Height = ground + und + c.MicHeight
	}
	p := geo.ECEF(lat, lon, r.Height)
	for j, e := range c.Events {
		for _, t := range ts {
			d := FitDiagnostic{Track: t.ID, Event: j + 1, Rejections: []string{}, Points: []FitResidual{}}
			if c.ExcludeJets {
				kept, _ := tracks.ExcludeKnownJets([]tracks.Track{t})
				if len(kept) == 0 {
					d.Rejections = append(d.Rejections, "excluded_known_jet")
				}
			}
			a, ok := t.CPA(p, c.SoundSpeed, c.MaxGap)
			if !ok {
				d.Rejections = append(d.Rejections, "no_valid_cpa")
			} else {
				d.CPA = &a
				if e.Partial {
					a, ok = t.ApproachWithin(p, epoch-c.ClockUncertainty+e.Start, epoch+c.ClockUncertainty+e.End, c.SoundSpeed, c.MaxGap)
				}
				if !ok || a.Distance < e.MinSlant || a.Distance > e.MaxSlant {
					d.Rejections = append(d.Rejections, "slant_distance")
				}
				if a.Speed < e.MinSpeed || a.Speed > e.MaxSpeed {
					d.Rejections = append(d.Rejections, "speed")
				}
				if !eventAtClock(e, a, epoch+offset) {
					d.Rejections = append(d.Rejections, "delayed_cpa_timing")
				}
			}
			scoreObservedTrackDiagnostic(t, p, s, e, epoch+offset, c.SoundSpeed, c.MaxGap, prepareObservations(s, e), &d)
			if d.Reason != "scored" {
				d.Rejections = append(d.Rejections, d.Reason)
			} else if *d.Score < c.MinScore {
				d.Rejections = append(d.Rejections, "spectral_score")
			}
			for _, q := range e.Ridge {
				factor, _, ok := t.Received(p, epoch+offset+q.Time, c.SoundSpeed, c.MaxGap)
				if ok && d.F0 > 0 {
					pred := d.F0 * factor
					d.Points = append(d.Points, FitResidual{q.Time, q.Hz, pred, q.Hz - pred, q.Hz / factor})
				}
			}
			if d.F0 > 0 {
				d.FullResolution = measureFitSupport(t, p, s, e, epoch+offset, c.SoundSpeed, c.MaxGap, d.F0)
			}
			r.Fits = append(r.Fits, d)
		}
	}
	return r, nil
}

func measureFitSupport(t tracks.Track, p geo.Vec, s *acoustic.Spectrum, e acoustic.Event, start, c, gap, f0 float64) *FitSupport {
	r := &FitSupport{}
	var residuals, contrasts []float64
	squared := 0.0
	first := max(0, int(math.Ceil((e.Start-s.Time(0))/s.Step())))
	last := min(len(s.Rows)-1, int(math.Floor((e.End-s.Time(0))/s.Step())))
	for frame := first; frame <= last; frame++ {
		time := s.Time(frame)
		factor, _, ok := t.Received(p, start+time, c, gap)
		if !ok {
			continue
		}
		r.Frames++
		pred := f0 * factor
		if observed, ok := e.RidgeHz(time); ok {
			delta := observed - pred
			squared += delta * delta
			residuals = append(residuals, math.Abs(delta))
		}
		for _, harmonic := range e.Harmonics {
			hz := pred * float64(harmonic)
			v, ok := s.Value(frame, hz)
			if !ok {
				continue
			}
			width := math.Max(12, math.Max(hz*.025, 3*float64(s.SampleRate)/float64(s.Window)))
			var side []float64
			for _, sign := range []float64{-1, 1} {
				for k := 0; k < 5; k++ {
					if x, ok := s.Value(frame, hz+sign*width*(.5+float64(k)/8)); ok {
						side = append(side, x)
					}
				}
			}
			if len(side) == 10 {
				contrasts = append(contrasts, v-diagnosticMedian(side))
			}
		}
	}
	r.RidgeSamples = len(residuals)
	if len(residuals) > 0 {
		rmse, med := math.Sqrt(squared/float64(len(residuals))), diagnosticMedian(residuals)
		r.RMSE, r.MedianAbsResidual = &rmse, &med
	}
	r.ContrastFrames = len(contrasts)
	if len(contrasts) > 0 {
		positive := 0
		for _, v := range contrasts {
			if v > 0 {
				positive++
			}
		}
		med, fraction := diagnosticMedian(contrasts), float64(positive)/float64(len(contrasts))
		r.MedianContrast, r.PositiveContrastFraction = &med, &fraction
	}
	return r
}

func diagnosticMedian(values []float64) float64 {
	sort.Float64s(values)
	n := len(values)
	if n%2 == 1 {
		return values[n/2]
	}
	return (values[n/2-1] + values[n/2]) / 2
}
