package search

import (
	"fmt"
	"pinpoint/internal/acoustic"
	"pinpoint/internal/geo"
	"pinpoint/internal/tracks"
	"time"
)

type Evidence struct {
	Source     string          `json:"source"`
	Method     string          `json:"method"`
	Note       string          `json:"note"`
	Events     []EventEvidence `json:"events"`
	Candidates []Candidate     `json:"candidates"`
}
type EventEvidence struct {
	Event       acoustic.Event         `json:"event"`
	StartOffset float64                `json:"start_offset_from_first_s"`
	Spectrum    acoustic.SpectralImage `json:"spectrum"`
	Models      []ModelCurve           `json:"models"`
}
type ModelCurve struct {
	Validation *SequenceCheck           `json:"reserved_validation,omitempty"`
	CPA        float64                  `json:"delayed_cpa_s"`
	Track      string                   `json:"track_id"`
	Score      float64                  `json:"score_db"`
	F0         float64                  `json:"f0_hz"`
	Segments   [][]acoustic.RidgeSample `json:"segments"`
	Support    *FitSupport              `json:"fit_support,omitempty"`
	Timing     *RelativeTiming          `json:"relative_timing,omitempty"`
}

// RelativeTiming compares measured arrival CPAs, never emission times. Partial
// curves have fixed sample timestamps but no measured CPA or CPA-gap claim.
type RelativeTiming struct {
	Reference int     `json:"reference_event"`
	Observed  float64 `json:"observed_gap_s"`
	Predicted float64 `json:"predicted_gap_s"`
	Residual  float64 `json:"residual_s"`
	Tolerance float64 `json:"tolerance_s"`
}

// BuildEvidence draws predictions from the stored winning hypotheses. It never
// moves individual events to improve their visual alignment: a joint candidate
// has one location and one recording clock shared by all its event curves.
func BuildEvidence(r Report, c Config, s *acoustic.Spectrum, ts []tracks.Track) (Evidence, error) {
	out := Evidence{Source: s.Source, Method: "Single-event comparison", Note: "White: recorded spectral power. Cyan: independently detected tonal ridge. Orange: predicted sound from the flight trajectory, including travel time. Scores are uncalibrated fit measures.", Candidates: r.FlightGroups, Events: []EventEvidence{}}
	if len(out.Candidates) == 0 {
		out.Candidates = r.Candidates
	}
	if len(c.Events) > 1 {
		out.Method = "Joint comparison: one observer position and one recording clock for all events"
	}
	base, _ := time.Parse(time.RFC3339Nano, c.RecordingStart)
	epoch := float64(base.Unix()) + float64(base.Nanosecond())/1e9
	byID := map[string]tracks.Track{}
	for _, t := range ts {
		byID[t.ID] = t
	}
	for i, e := range c.Events {
		source := eventSpectrum(c, s, i)
		img, err := source.EventImage(e)
		if err != nil {
			return Evidence{}, err
		}
		event := EventEvidence{Event: e, StartOffset: e.Start - c.Events[0].Start, Spectrum: img, Models: []ModelCurve{}}
		for _, candidate := range out.Candidates {
			if i >= len(candidate.Events) {
				return Evidence{}, fmt.Errorf("incomplete candidate event metadata")
			}
			match := candidate.Events[i]
			model := ModelCurve{Track: match.Track, Score: match.Score, F0: match.F0, CPA: match.CPA.Heard - epoch - candidate.ClockOffset, Segments: [][]acoustic.RidgeSample{}}
			if v := c.validationCandidate; v != nil && len(c.validationChecks) == len(c.Events) && v.Lat == candidate.Lat && v.Lon == candidate.Lon && v.Height == candidate.Height && v.ClockOffset == candidate.ClockOffset && v.Events[i].Track == match.Track && v.Events[i].F0 == match.F0 {
				model.Validation = &c.validationChecks[i]
			}
			if !e.Partial && e.CPATolerance > 0 {
				for previous := i - 1; previous >= 0; previous-- {
					ref := c.Events[previous]
					if ref.Partial || ref.CPATolerance <= 0 {
						continue
					}
					observed := e.CPATime - ref.CPATime
					predicted := match.CPA.Heard - candidate.Events[previous].CPA.Heard
					model.Timing = &RelativeTiming{previous + 1, observed, predicted, predicted - observed, e.CPATolerance + ref.CPATolerance}
					break
				}
			}
			track, ok := byID[match.Track]
			if !ok {
				return Evidence{}, fmt.Errorf("trajectory %s is unavailable for evidence", match.Track)
			}
			observer := geo.ECEF(candidate.Lat, candidate.Lon, candidate.Height)
			model.Support = measureFitSupport(track, observer, source, e, epoch+candidate.ClockOffset, c.SoundSpeed, c.MaxGap, match.F0)
			var segment []acoustic.RidgeSample
			flush := func() {
				if len(segment) > 0 {
					model.Segments = append(model.Segments, segment)
					segment = nil
				}
			}
			first := max(0, int((img.Start-source.Time(0))/source.Step()))
			for frame := first; frame < len(source.Rows) && source.Time(frame) <= img.End; frame++ {
				t := source.Time(frame)
				factor, _, valid := track.Received(observer, epoch+candidate.ClockOffset+t, c.SoundSpeed, c.MaxGap)
				if !valid {
					flush()
					continue
				}
				segment = append(segment, acoustic.RidgeSample{Time: t, Hz: match.F0 * factor})
			}
			flush()
			event.Models = append(event.Models, model)
		}
		out.Events = append(out.Events, event)
	}
	return out, nil
}
