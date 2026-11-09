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

// ReceiverHypothesis is disclosed input for supervised recognition, not a
// location estimate. Every aircraft uses exactly the same supplied clock.
type ReceiverHypothesis struct {
	Lat            float64 `json:"lat"`
	Lon            float64 `json:"lon"`
	Height         float64 `json:"height_ellipsoid_m"`
	RecordingStart string  `json:"recording_start"`
	Clock          float64 `json:"clock_correction_s"`
	SoundSpeed     float64 `json:"sound_speed_mps"`
	MaxGap         float64 `json:"max_track_gap_s"`
}

type ReceiverReview struct {
	Source    string             `json:"source"`
	Receiver  ReceiverHypothesis `json:"supplied_receiver"`
	Options   GuidedOptions      `json:"options"`
	Matches   []GuidedMatch      `json:"matches"`
	Supported int                `json:"experimental_supported_windows"`
	Note      string             `json:"note"`
}

// ReviewReceiver tests predicted flight windows even without an automatic
// tonal proposal. It shares Corroborate's spectral/steady-tone/time controls,
// but requires no acoustic anchor because the receiver is explicitly supplied.
// It never searches positions, refits the clock, promotes detections, or creates
// a location candidate. No-support is not evidence of silence or no aircraft.
func ReviewReceiver(ctx context.Context, h ReceiverHypothesis, s *acoustic.Spectrum, ts []tracks.Track, o GuidedOptions) (ReceiverReview, error) {
	r := ReceiverReview{Receiver: h, Options: o, Matches: []GuidedMatch{}, Note: "Supervised acoustic review at a supplied receiver and one fixed clock; not location inference or independently confirmed aircraft identification. Predicted arrival times and inter-flight offsets remain fixed. Curved-versus-stationary training evidence selects pitch/window; separate blocks assess support. Distant shifted-audio controls are used when available. Short clips use same-audio reversed/shifted trajectory and linear-tone alternatives; these are correlated checks, not independent null trials or calibrated probabilities. Partial and unavailable windows are explicit. No-support does not establish aircraft absence or silence. No automatic proposal or acoustic anchor is required. Repeated circuits remaining continuously in range are one modeled encounter."}
	if s == nil || len(s.Rows) == 0 || len(s.Rows[0]) < 3 || s.Window <= 0 || s.SampleRate <= 0 || s.Hop <= 0 {
		return r, fmt.Errorf("invalid review spectrum")
	}
	r.Source = s.Source
	if err := validateReviewIntervals(o, s); err != nil {
		return r, err
	}
	start, err := time.Parse(time.RFC3339Nano, h.RecordingStart)
	if err != nil {
		return r, fmt.Errorf("recording_start requires an explicit time zone: %w", err)
	}
	for _, v := range []float64{h.Lat, h.Lon, h.Height, h.Clock, h.SoundSpeed, h.MaxGap, o.MinHz, o.MaxHz, o.StepHz, o.MaxSlant, o.MinContrast, o.ControlMargin} {
		if !geo.Finite(v) {
			return r, fmt.Errorf("nonfinite receiver review setting")
		}
	}
	if math.Abs(h.Lat) > 85 || math.Abs(h.Lon) > 180 || h.Height < -1000 || h.Height > 10000 || math.Abs(h.Clock) > 3600 || h.SoundSpeed < 300 || h.SoundSpeed > 370 || h.MaxGap <= 0 || h.MaxGap > 120 || o.MinHz <= 0 || o.MaxHz < o.MinHz || o.StepHz <= 0 || (o.MaxHz-o.MinHz)/o.StepHz > 20000 || o.MaxSlant <= 0 || o.MaxSlant > 50000 || o.MinContrast <= 0 || o.ControlMargin < 0 || o.MaxPasses < 1 || o.MaxPasses > 2000 || o.SequenceControls != 0 {
		return r, fmt.Errorf("invalid supplied receiver or review limits")
	}
	if o.FoldOrigin != nil && !geo.Finite(*o.FoldOrigin) {
		return r, fmt.Errorf("nonfinite fold origin")
	}
	c := Config{SoundSpeed: h.SoundSpeed, MaxGap: h.MaxGap}
	p := observer{lat: h.Lat, lon: h.Lon, height: h.Height, pos: geo.ECEF(h.Lat, h.Lon, h.Height)}
	epoch := float64(start.Unix()) + float64(start.Nanosecond())/1e9 + h.Clock
	result, err := guidedScheduleAt(ctx, c, s, ts, GuidedCandidate{Matches: []GuidedMatch{}}, p, epoch, o, nil, nil, 0, nil, true)
	r.Matches = result.Matches
	r.Supported = result.Supported
	if err != nil {
		return r, err
	}
	return r, ctx.Err()
}
