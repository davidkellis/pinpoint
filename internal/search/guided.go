package search

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"

	"pinpoint/internal/acoustic"
	"pinpoint/internal/geo"
	"pinpoint/internal/tracks"
)

// GuidedOptions controls a follow-up to a seeded location search. Thresholds
// are explicit research heuristics, not calibrated confidence probabilities.
type GuidedOptions struct {
	MinHz            float64          `json:"min_source_hz"`
	MaxHz            float64          `json:"max_source_hz"`
	StepHz           float64          `json:"source_step_hz"`
	MaxSlant         float64          `json:"max_slant_m"`
	MinContrast      float64          `json:"min_contrast_db"`
	ControlMargin    float64          `json:"control_margin_db"`
	MaxCandidates    int              `json:"max_candidates"`
	MaxPasses        int              `json:"max_passes_per_candidate"`
	Workers          int              `json:"workers"`
	SequenceControls int              `json:"sequence_control_trials,omitempty"`
	SequenceExtra    int              `json:"sequence_extra_flights,omitempty"`
	FullCurves       bool             `json:"full_curve_windows,omitempty"`
	FoldOrigin       *float64         `json:"fixed_fold_origin_audio_s,omitempty"`
	ReviewIntervals  []ReviewInterval `json:"review_intervals,omitempty"`
	LinearControls   bool             `json:"compare_linear_tone_all_windows,omitempty"`
	TrainingOnly     bool             `json:"training_only_skip_confirmation,omitempty"`
}

// ReviewInterval identifies one observed sound, independently of a flight/GPS.
// The rest of the recording remains available for wrong-time comparisons.
type ReviewInterval struct {
	Start float64 `json:"start_s"`
	End   float64 `json:"end_s"`
}

func validateReviewIntervals(o GuidedOptions, s *acoustic.Spectrum) error {
	if len(o.ReviewIntervals) > 128 {
		return fmt.Errorf("at most 128 review intervals are allowed")
	}
	for i, w := range o.ReviewIntervals {
		if !geo.Finite(w.Start) || !geo.Finite(w.End) || w.Start < 0 || w.End <= w.Start || w.End > s.Time(len(s.Rows)-1)+s.Step() || i > 0 && w.Start < o.ReviewIntervals[i-1].End {
			return fmt.Errorf("review intervals must be finite, ordered, nonoverlapping and inside the recording")
		}
	}
	if len(o.ReviewIntervals) > 0 && o.SequenceControls > 0 {
		return fmt.Errorf("review intervals are not supported by the paired-cohort sequence audit")
	}
	return nil
}

func DefaultGuidedOptions() GuidedOptions {
	return GuidedOptions{MinHz: 40, MaxHz: 6000, StepHz: .5, MaxSlant: 15000, MinContrast: 3, ControlMargin: 1, MaxCandidates: 24, MaxPasses: 512, Workers: 4, SequenceExtra: 1, LinearControls: true}
}

type GuidedFit struct {
	F0          float64 `json:"f0_hz"`
	Train       float64 `json:"training_contrast_db"`
	Heldout     float64 `json:"heldout_contrast_db"`
	Positive    float64 `json:"heldout_positive_fraction"`
	Coverage    float64 `json:"coverage"`
	Valid       bool    `json:"valid"`
	Selection   float64 `json:"training_selection_db"`
	TrainMargin float64 `json:"training_steady_margin_db"`
	Slope       float64 `json:"slope_hz_per_s,omitempty"`
	Reference   float64 `json:"reference_audio_s,omitempty"`
}
type GuidedControl struct {
	Shift         float64   `json:"audio_shift_s"`
	Fit           GuidedFit `json:"fit"`
	Kind          string    `json:"kind,omitempty"`
	GeometryShift float64   `json:"geometry_shift_s,omitempty"`
}
type GuidedMatch struct {
	Track        string                 `json:"track_id"`
	Type         string                 `json:"type_code,omitempty"`
	CPA          float64                `json:"predicted_audio_cpa_s"`
	AnchorGap    float64                `json:"predicted_gap_from_first_anchor_cpa_s,omitempty"`
	Start        float64                `json:"start_s"`
	End          float64                `json:"end_s"`
	Distance     float64                `json:"slant_m"`
	Fit          GuidedFit              `json:"fit"`
	Flat         GuidedFit              `json:"steady_tone_control"`
	Linear       *GuidedFit             `json:"linear_tone_control,omitempty"`
	FlatBand     [2]float64             `json:"steady_tone_band_hz"`
	Controls     []GuidedControl        `json:"shifted_time_controls"`
	Margin       float64                `json:"heldout_control_margin_db"`
	Supported    bool                   `json:"additional_support"`
	Reasons      []string               `json:"reasons"`
	Curve        []acoustic.RidgeSample `json:"predicted_curve"`
	Partial      bool                   `json:"partial_window,omitempty"`
	Validation   string                 `json:"validation_status"`
	ControlMode  string                 `json:"control_mode,omitempty"`
	Alternatives []GuidedControl        `json:"same_audio_controls,omitempty"`
	Interval     int                    `json:"review_interval,omitempty"`
}
type GuidedCandidate struct {
	Seed        Candidate     `json:"seed"`
	AnchorValid bool          `json:"anchor_rechecked"`
	Supported   int           `json:"additional_flights_with_support"`
	Matches     []GuidedMatch `json:"matches"`
}
type GuidedReport struct {
	Version    int               `json:"version"`
	Source     string            `json:"source"`
	Note       string            `json:"note"`
	SeedNote   string            `json:"seed_search_note,omitempty"`
	Options    GuidedOptions     `json:"options"`
	Candidates []GuidedCandidate `json:"candidates"`
	Sequence   *SequenceAudit    `json:"sequence_audit,omitempty"`
}

// Corroborate searches audio OUTSIDE the seed events. Position, clock and
// initial-flight assignment remain frozen. No weak detection is promoted and
// no anchor map is overwritten. Shifted controls never overlap anchor audio.
func Corroborate(ctx context.Context, c Config, s *acoustic.Spectrum, ts []tracks.Track, seeds []Candidate, o GuidedOptions, progress func(int, int)) (GuidedReport, error) {
	// Candidate positions/CPAs must never redefine which audio is reserved.
	// A supplied-receiver diagnostic may use a local fold, but a comparison of
	// locations uses one recording-wide partition for every candidate/control.
	if o.FoldOrigin == nil {
		origin := 0.
		o.FoldOrigin = &origin
	}
	r := GuidedReport{Version: 2, Source: s.Source, Options: o, Note: "Exploratory follow-up: each seed position and shared clock are fixed. Source tone/window selection compares curved and stationary training explanations. Separate blocks and shifted-audio controls assess support; short clips use matched same-audio trajectory and linear-tone alternatives, explicitly identified in each fit. Those correlated checks are not independent null trials or calibrated probabilities. Partial/unavailable windows are explicit. Anchor locations can already be biased. No-support does not mean no aircraft or prove silence. Only supplied seeds and trajectories are examined; undiscovered locations are not searched.", Candidates: []GuidedCandidate{}}
	epoch, err := c.Validate(s)
	if err != nil {
		return r, err
	}
	if err = validateReviewIntervals(o, s); err != nil {
		return r, err
	}
	for _, v := range []float64{o.MinHz, o.MaxHz, o.StepHz, o.MaxSlant, o.MinContrast, o.ControlMargin} {
		if !geo.Finite(v) {
			return r, fmt.Errorf("nonfinite guided-search settings")
		}
	}
	if o.MinHz <= 0 || o.MaxHz < o.MinHz || o.StepHz <= 0 || (o.MaxHz-o.MinHz)/o.StepHz > 20000 || o.MaxSlant <= 0 || o.MaxSlant > 50000 || o.MinContrast <= 0 || o.ControlMargin < 0 || o.MaxCandidates < 1 || o.MaxCandidates > 256 || o.MaxPasses < 1 || o.MaxPasses > 2000 || o.Workers < 1 || o.Workers > 32 {
		return r, fmt.Errorf("invalid guided-search limits")
	}
	if o.FoldOrigin != nil && !geo.Finite(*o.FoldOrigin) {
		return r, fmt.Errorf("nonfinite fold origin")
	}
	if len(seeds) == 0 {
		return r, fmt.Errorf("no seed hypotheses; run a location search first")
	}
	if o.SequenceControls != 0 && (o.SequenceControls < 5 || o.SequenceControls > 39 || o.SequenceExtra < 1 || o.SequenceExtra > 4) {
		return r, fmt.Errorf("sequence-controls must be zero or 5–39, and sequence-extra 1–4")
	}
	for _, p := range seeds {
		if !geo.Finite(p.Lat) || !geo.Finite(p.Lon) || !geo.Finite(p.Height) || !geo.Finite(p.ClockOffset) || !geo.Finite(p.Score) || !c.Bounds.Contains(p.Lat, p.Lon) || math.Abs(p.ClockOffset) > c.ClockUncertainty || len(p.Events) != len(c.Events) {
			return r, fmt.Errorf("seed does not belong to the supplied search bounds, clock or event count")
		}
	}
	// Retain competing flight groups before filling with separated spatial peaks.
	seeds = append([]Candidate(nil), seeds...)
	sortCandidates(seeds)
	if o.SequenceControls > 0 {
		seeds = followupSeeds(seeds, 500, 5, o.MaxCandidates)
	} else {
		seeds = refinementSeeds(seeds, math.Max(25, c.Refine), o.MaxCandidates)
	}
	if c.ExcludeJets {
		ts, _ = tracks.ExcludeKnownJets(ts)
	}
	r.Candidates = make([]GuidedCandidate, len(seeds))
	type result struct {
		i     int
		value GuidedCandidate
		err   error
	}
	jobs := make(chan int)
	done := make(chan result, o.Workers)
	var wg sync.WaitGroup
	for w := 0; w < o.Workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				v, e := corroborateSeed(ctx, c, s, ts, seeds[i], epoch, o)
				done <- result{i, v, e}
			}
		}()
	}
	go func() {
		defer close(jobs)
		for i := range seeds {
			select {
			case jobs <- i:
			case <-ctx.Done():
				return
			}
		}
	}()
	go func() { wg.Wait(); close(done) }()
	n := 0
	var failure error
	for v := range done {
		r.Candidates[v.i] = v.value
		n++
		if v.err != nil && failure == nil {
			failure = v.err
		}
		if progress != nil {
			progress(n, len(seeds))
		}
	}
	if err = ctx.Err(); err != nil {
		return r, err
	}
	if failure != nil {
		return r, failure
	}
	// Sort only for review. Original scores and positions are preserved, and
	// there is no automatic conversion into a confirmed location or a new label.
	sort.SliceStable(r.Candidates, func(i, j int) bool { return r.Candidates[i].Supported > r.Candidates[j].Supported })
	if o.SequenceControls > 0 {
		audit, err := auditSequences(ctx, c, s, ts, r.Candidates, epoch, o, progress)
		if err != nil {
			return r, err
		}
		r.Sequence = &audit
	}
	return r, nil
}

func overlapsAnchors(c Config, start, end, guard float64) bool {
	for _, e := range c.Events {
		if start < e.End+guard && end > e.Start-guard {
			return true
		}
	}
	return false
}

func corroborateSeed(ctx context.Context, c Config, s *acoustic.Spectrum, ts []tracks.Track, seed Candidate, epoch float64, o GuidedOptions) (GuidedCandidate, error) {
	return guidedSeedAt(ctx, c, s, ts, seed, epoch, o, 0, nil, true)
}

func guidedSeedAt(ctx context.Context, c Config, s *acoustic.Spectrum, ts []tracks.Track, seed Candidate, epoch float64, o GuidedOptions, shift float64, pairedShifts []float64, localControls bool) (GuidedCandidate, error) {
	r := GuidedCandidate{Seed: seed, Matches: []GuidedMatch{}}
	p := observer{lat: seed.Lat, lon: seed.Lon, height: seed.Height, pos: geo.ECEF(seed.Lat, seed.Lon, seed.Height)}
	locked := c
	locked.clockOffsets = []float64{seed.ClockOffset}
	locked.eventTrackIDs = make([]string, len(seed.Events))
	used := map[string]bool{}
	for i, m := range seed.Events {
		locked.eventTrackIDs[i] = m.Track
		used[guidedAircraftID(m.Track)] = true
	}
	refit, _, ok := evaluatePoint(ctx, p, locked, s, ts, epoch)
	if !ok {
		return r, ctx.Err()
	}
	r.AnchorValid = true
	anchorHeard := refit.Events[0].CPA.Heard
	return guidedScheduleAt(ctx, c, s, ts, r, p, epoch+seed.ClockOffset-shift, o, used, &anchorHeard, shift, pairedShifts, localControls)
}

// guidedScheduleAt compares one fixed receiver/clock with the flight schedule.
// The anchored caller rechecks its original events before entering here. A
// separate supervised receiver review can call it without inventing an anchor.
func guidedScheduleAt(ctx context.Context, c Config, s *acoustic.Spectrum, ts []tracks.Track, r GuidedCandidate, p observer, recordStart float64, o GuidedOptions, used map[string]bool, anchorHeard *float64, shift float64, pairedShifts []float64, localControls bool) (GuidedCandidate, error) {
	guard := float64(s.Window) / float64(s.SampleRate)
	first, last := s.Time(0), s.Time(len(s.Rows)-1)
	for _, t := range ts {
		if err := ctx.Err(); err != nil {
			return r, err
		}
		if used[guidedAircraftID(t.ID)] {
			continue
		}
		// One closest approach per continuous in-range encounter. Repeated
		// circuits that never leave the sphere are currently one hypothesis.
		for _, interval := range t.WithinRadius(p.pos, o.MaxSlant, c.SoundSpeed, c.MaxGap) {
			if interval.End < recordStart+first || interval.Start > recordStart+last {
				continue
			}
			a, ok := t.ApproachWithin(p.pos, interval.Start, interval.End, c.SoundSpeed, c.MaxGap)
			if !ok {
				continue
			}
			center := a.Heard - recordStart
			if len(o.ReviewIntervals) > 0 {
				overlap := false
				for _, w := range o.ReviewIntervals {
					if center+60 > w.Start && center-60 < w.End {
						overlap = true
						break
					}
				}
				if !overlap {
					continue
				}
			}
			if len(pairedShifts) > 0 && !sequenceEligible(c, s, center-shift, pairedShifts) {
				continue
			}
			m := GuidedMatch{Track: t.ID, Type: t.TypeCode, CPA: center, Distance: a.Distance, Reasons: []string{}, Controls: []GuidedControl{}, Curve: []acoustic.RidgeSample{}}
			if anchorHeard != nil {
				m.AnchorGap = a.Heard - *anchorHeard
			}
			// Choose the tone/window using training scores only. Test scores are
			// never used to change source pitch, time placement or receiver position.
			for _, half := range []float64{20, 60} {
				if o.FullCurves && half < 60 {
					continue
				}
				intervals := o.ReviewIntervals
				if len(intervals) == 0 {
					intervals = []ReviewInterval{{Start: first, End: last}}
				}
				for intervalIndex, interval := range intervals {
					from, to := math.Max(center-half, interval.Start), math.Min(center+half, interval.End)
					partial := from != center-half || to != center+half || from < first || to > last
					if o.FullCurves && partial {
						continue
					}
					from, to = math.Max(first, from), math.Min(last, to)
					if to <= from || overlapsAnchors(c, from, to, guard) {
						continue
					}
					f, err := guidedFit(ctx, c, s, t, p.pos, recordStart, from, to, 0, false, o)
					if err != nil {
						return r, err
					}
					if f.Valid && (!m.Fit.Valid || f.Selection > m.Fit.Selection) {
						m.Fit = f
						m.Start = from
						m.End = to
						m.Partial = partial
						if len(o.ReviewIntervals) > 0 {
							m.Interval = intervalIndex + 1
						}
					}
				}
			}
			if !m.Fit.Valid {
				m.Validation = "unavailable"
				m.Reasons = append(m.Reasons, "no_eligible_model_or_disjoint_samples")
				if len(r.Matches) >= o.MaxPasses {
					return r, fmt.Errorf("guided pass limit exceeded; narrow the input recording or increase max-passes")
				}
				r.Matches = append(r.Matches, m)
				continue
			}
			if len(r.Matches) >= o.MaxPasses {
				return r, fmt.Errorf("guided pass limit exceeded; narrow the input recording or increase max-passes")
			}
			if o.TrainingOnly {
				m.Validation = "not_evaluated_training_only"
				m.Reasons = append(m.Reasons, "confirmation_deferred_until_training_winner_is_frozen")
				r.Matches = append(r.Matches, m)
				continue
			}
			minFactor, maxFactor := math.Inf(1), 0.
			for sec := m.Start; sec <= m.End; sec += math.Max(.5, s.Step()) {
				factor, _, valid := t.Received(p.pos, recordStart+sec, c.SoundSpeed, c.MaxGap)
				if !valid {
					continue
				}
				minFactor = math.Min(minFactor, factor)
				maxFactor = math.Max(maxFactor, factor)
				m.Curve = append(m.Curve, acoustic.RidgeSample{Time: sec, Hz: factor * m.Fit.F0})
			}
			// Compare a stationary explanation of this frequency band. A louder,
			// unrelated hum elsewhere does not explain or disprove a curved ridge.
			// Bounds depend only on geometry and the training-selected source tone.
			flatOptions := o
			df := float64(s.SampleRate) / float64(s.Window)
			flatOptions.MinHz = math.Max(s.Hz(0), minFactor*m.Fit.F0-2*df)
			flatOptions.MaxHz = math.Min(s.Hz(len(s.Rows[0])-1), maxFactor*m.Fit.F0+2*df)
			flatOptions.StepHz = math.Max(o.StepHz, (flatOptions.MaxHz-flatOptions.MinHz)/20000)
			m.FlatBand = [2]float64{flatOptions.MinHz, flatOptions.MaxHz}
			var err error
			m.Flat, err = guidedFit(ctx, c, s, t, p.pos, recordStart, m.Start, m.End, 0, true, flatOptions)
			if err != nil {
				return r, err
			}
			bestControl := -math.MaxFloat64
			if m.Flat.Valid {
				bestControl = m.Flat.Heldout
			}
			for _, shift := range []float64{-480, -240, -120, 120, 240, 480} {
				if !localControls {
					break
				}
				// Do not reuse this actual passage or the seed audio as a control.
				if math.Abs(shift) <= m.End-m.Start+guard || m.Start+shift < first || m.End+shift > last || overlapsAnchors(c, m.Start+shift, m.End+shift, guard) {
					continue
				}
				f, e := guidedFit(ctx, c, s, t, p.pos, recordStart, m.Start, m.End, shift, false, o)
				if e != nil {
					return r, e
				}
				if f.Valid {
					m.Controls = append(m.Controls, GuidedControl{Shift: shift, Fit: f, Kind: "shifted_audio"})
					bestControl = math.Max(bestControl, f.Heldout)
				}
			}
			m.ControlMode = "shifted_audio"
			controlsAvailable := len(m.Controls) >= 3
			if localControls && !controlsAvailable {
				// A short recording has no distant background windows. Compare
				// competing trajectory shapes on the SAME audio, with the same
				// source-pitch search and training/validation split. These are
				// correlated hypothesis checks, never independent null trials or a
				// calibrated false-positive probability.
				span := m.End - m.Start
				models := []guidedGeometry{{reverse: true}, {offset: -2 * span}, {offset: -span}, {offset: span}, {offset: 2 * span}}
				reversedAvailable := false
				for _, model := range models {
					f, e := guidedFitGeometry(ctx, c, s, t, p.pos, recordStart, m.Start, m.End, 0, false, o, model)
					if e != nil {
						return r, e
					}
					if !f.Valid {
						continue
					}
					kind := "shifted_trajectory"
					if model.reverse {
						kind = "reversed_trajectory"
						reversedAvailable = true
					}
					m.Alternatives = append(m.Alternatives, GuidedControl{Fit: f, Kind: kind, GeometryShift: model.offset})
					bestControl = math.Max(bestControl, f.Heldout)
				}
				controlsAvailable = len(m.Alternatives) >= 3 && reversedAvailable
				m.ControlMode = "same_audio_geometry"
				line := guidedLinear(ctx, s, m.Start, m.End, m.FlatBand, o.FoldOrigin)
				m.Linear = &line
				if line.Valid {
					bestControl = math.Max(bestControl, line.Heldout)
				} else {
					controlsAvailable = false
				}
			}
			// Distant wrong-time controls do not distinguish a curved flyover
			// from an unrelated linear chirp in this window. Test that competing
			// explanation on the same audio even for a long recording.
			if o.LinearControls && m.Linear == nil {
				line := guidedLinear(ctx, s, m.Start, m.End, m.FlatBand, o.FoldOrigin)
				m.Linear = &line
				if line.Valid {
					bestControl = math.Max(bestControl, line.Heldout)
				} else {
					controlsAvailable = false
				}
			}
			if m.Fit.Train < o.MinContrast || m.Fit.Heldout < o.MinContrast {
				m.Reasons = append(m.Reasons, "training_or_heldout_contrast")
			}
			if m.Fit.Positive < .65 {
				m.Reasons = append(m.Reasons, "heldout_continuity")
			}
			if !controlsAvailable || !m.Flat.Valid {
				m.Reasons = append(m.Reasons, "insufficient_controls")
			}
			if bestControl > -math.MaxFloat64 {
				m.Margin = m.Fit.Heldout - bestControl
			}
			if m.Margin < o.ControlMargin {
				m.Reasons = append(m.Reasons, "competing_control")
			}
			if maxFactor/minFactor < 1.03 {
				m.Reasons = append(m.Reasons, "nearly_stationary_frequency")
			}
			m.Supported = localControls && len(m.Reasons) == 0
			m.Validation = "rejected"
			if !controlsAvailable || !m.Flat.Valid {
				m.Validation = "unavailable"
			}
			if m.Supported {
				m.Validation = "experimental_support"
			}
			r.Matches = append(r.Matches, m)
		}
	}
	// One piece of audio/aircraft cannot count twice. Retain alternatives for
	// inspection, but count only nonoverlapping, distinct supported flights.
	sort.SliceStable(r.Matches, func(i, j int) bool { return r.Matches[i].Fit.Selection > r.Matches[j].Fit.Selection })
	var accepted []GuidedMatch
	for i := range r.Matches {
		m := &r.Matches[i]
		if !m.Supported {
			continue
		}
		for _, a := range accepted {
			if guidedAircraftID(m.Track) == guidedAircraftID(a.Track) || m.Interval > 0 && m.Interval == a.Interval || m.Start < a.End+guard && m.End > a.Start-guard {
				m.Supported = false
				m.Validation = "duplicate_or_overlapping"
				m.Reasons = append(m.Reasons, "duplicate_or_overlapping_evidence")
				break
			}
		}
		if m.Supported {
			accepted = append(accepted, *m)
			r.Supported++
		}
	}
	return r, nil
}

// The trace importer names segments ICAO-epoch-leg. Separate segments of that
// aircraft are not independent corroborating aircraft. Other providers' opaque
// IDs remain distinct; they need explicit identity metadata to deduplicate.
func guidedAircraftID(id string) string {
	parts := strings.Split(id, "-")
	if len(parts) == 3 && len(parts[0]) == 6 {
		_, hexErr := strconv.ParseUint(parts[0], 16, 24)
		epoch, timeErr := strconv.ParseFloat(parts[1], 64)
		_, legErr := strconv.ParseUint(parts[2], 10, 64)
		if hexErr == nil && timeErr == nil && geo.Finite(epoch) && legErr == nil {
			return strings.ToLower(parts[0])
		}
	}
	return id
}

type guidedSample struct {
	frame  int
	factor float64
	test   bool
}

func guidedFit(ctx context.Context, c Config, s *acoustic.Spectrum, t tracks.Track, p geo.Vec, epoch, from, to, shift float64, flat bool, o GuidedOptions) (GuidedFit, error) {
	return guidedFitGeometry(ctx, c, s, t, p, epoch, from, to, shift, flat, o, guidedGeometry{})
}

type guidedGeometry struct {
	offset  float64
	reverse bool
}

func guidedFitGeometry(ctx context.Context, c Config, s *acoustic.Spectrum, t tracks.Track, p geo.Vec, epoch, from, to, shift float64, flat bool, o GuidedOptions, geometry guidedGeometry) (GuidedFit, error) {
	var out GuidedFit
	window := float64(s.Window) / float64(s.SampleRate)
	block := math.Max(10, 4*window)
	guard := window / 2
	var points []guidedSample
	expected := 0
	for sec := from; sec <= to; sec += math.Max(1, s.Step()) {
		frame := int(math.Round((sec + shift - s.Time(0)) / s.Step()))
		if frame < 0 || frame >= len(s.Rows) {
			continue
		}
		actual := s.Time(frame) - shift
		origin, partitionTime := from, actual
		if o.FoldOrigin != nil {
			origin = *o.FoldOrigin
			partitionTime = s.Time(frame)
		}
		phase := math.Mod(partitionTime-origin, block)
		if phase < 0 {
			phase += block
		}
		if phase < guard || phase >= block-guard {
			continue
		}
		expected++
		geometryTime := actual + geometry.offset
		if geometry.reverse {
			geometryTime = from + to - actual
		}
		factor, _, ok := t.Received(p, epoch+geometryTime, c.SoundSpeed, c.MaxGap)
		if !ok {
			continue
		}
		if flat {
			factor = 1
		}
		points = append(points, guidedSample{frame, factor, int(math.Floor((partitionTime-origin)/block))%2 == 1})
	}
	if expected < 16 {
		return out, nil
	}
	out.Coverage = float64(len(points)) / float64(expected)
	if out.Coverage < .95 {
		return out, nil
	}
	// Frequency eligibility uses geometry from the entire window, never heldout
	// spectral power. Otherwise a training-selected tone can leave the stored
	// band in test blocks and silently change the control's encounter cohort.
	minFactor, maxFactor := math.Inf(1), 0.
	for _, q := range points {
		minFactor = math.Min(minFactor, q.factor)
		maxFactor = math.Max(maxFactor, q.factor)
	}
	df := float64(s.SampleRate) / float64(s.Window)
	// The wider default includes jet tones. Bound the actual sweep by the
	// available observed band and geometry, preserving the requested pitch-grid
	// origin and precision for low-frequency propeller recordings.
	searchMin := math.Max(o.MinHz, (s.Hz(0)+12*df)/minFactor)
	searchMax := math.Min(o.MaxHz, (s.Hz(len(s.Rows[0])-1)-12*df)/maxFactor)
	searchMin = o.MinHz + math.Max(0, math.Ceil((searchMin-o.MinHz)/o.StepHz))*o.StepHz
	if searchMax < searchMin {
		return out, nil
	}
	// Select against the strongest stationary explanation of EACH candidate's
	// observed band, before freezing a pitch. A single high-energy hum must not
	// occupy the only hypothesis that will later be validated. All pitch options
	// and both explanations see training blocks only.
	var stationary guidedStationaryScores
	if !flat {
		stationary = guidedStationary(ctx, s, points, minFactor*searchMin-2*df, maxFactor*searchMax+2*df, o.StepHz)
	}
	best := -math.MaxFloat64
	for f := searchMin; f <= searchMax+o.StepHz*.001; f += o.StepHz {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		lo, hi := minFactor*f, maxFactor*f
		if lo-2*math.Max(6*df, lo*.025) < s.Hz(0) || hi+2*math.Max(6*df, hi*.025) > s.Hz(len(s.Rows[0])-1) {
			continue
		}
		sum := 0.
		n := 0
		valid := true
		for _, q := range points {
			if q.test {
				continue
			}
			v, ok := guidedContrast(s, q.frame, q.factor*f)
			if !ok {
				valid = false
				break
			}
			sum += v
			n++
		}
		if !valid || n < 8 {
			continue
		}
		score := sum / float64(n)
		selection, margin := score, score
		if !flat {
			control, valid := stationary.maximum(lo-2*df, hi+2*df)
			if !valid {
				continue
			}
			margin = score - control
			selection = math.Min(score, margin)
		}
		if selection > best {
			best = selection
			out.F0 = f
			out.Train = score
			out.Selection = selection
			out.TrainMargin = margin
		}
	}
	if best == -math.MaxFloat64 {
		return out, nil
	}
	sum := 0.
	n, positive := 0, 0
	for _, q := range points {
		if !q.test {
			continue
		}
		v, ok := guidedContrast(s, q.frame, q.factor*out.F0)
		if !ok {
			return out, nil
		}
		sum += v
		n++
		if v > 0 {
			positive++
		}
	}
	if n < 8 {
		return out, nil
	}
	out.Heldout = sum / float64(n)
	out.Positive = float64(positive) / float64(n)
	out.Valid = true
	return out, nil
}

func guidedContrast(s *acoustic.Spectrum, frame int, hz float64) (float64, bool) {
	df := float64(s.SampleRate) / float64(s.Window)
	width := math.Max(6*df, hz*.025)
	v, ok := s.Value(frame, hz)
	if !ok {
		return 0, false
	}
	sum := 0.
	for _, offset := range []float64{-2 * width, -width, width, 2 * width} {
		x, valid := s.Value(frame, hz+offset)
		if !valid {
			return 0, false
		}
		sum += x
	}
	return math.Max(-15, math.Min(20, v-sum/4)), true
}
