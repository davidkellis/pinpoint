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

// AutomaticOptions controls the same complete pair search at actual and wrong
// recording times. Controls do not merely rescore the winning pair/location.
type AutomaticOptions struct {
	CacheDir      string    `json:"-"`
	ControlShifts []float64 `json:"control_shifts_s"`
	MaxPairs      int       `json:"max_pairs"`
	Margin        float64   `json:"required_margin_db"`
}

func DefaultAutomaticOptions(c Config) AutomaticOptions {
	d := math.Max(600, 2*c.ClockUncertainty+300)
	return AutomaticOptions{ControlShifts: []float64{-d, d}, MaxPairs: 512, Margin: 1}
}

type GroupProgress struct {
	Trial, Trials, Pair, Pairs int
	Shift                      float64
	Stage                      string
	Done, Total                int
}
type GroupResult struct {
	Trial     int        `json:"trial"`
	Pair      int        `json:"pair"`
	Events    [2]int     `json:"event_indices"`
	Shift     float64    `json:"time_shift_s"`
	Status    string     `json:"status"`
	Error     string     `json:"error,omitempty"`
	Best      *Candidate `json:"best,omitempty"`
	Locations int        `json:"locations_evaluated"`
	Ambiguous bool       `json:"ambiguous"`
}
type AutomaticReport struct {
	InputFingerprint string           `json:"search_inputs_sha256,omitempty"`
	Started          string           `json:"started_utc,omitempty"`
	Finished         string           `json:"finished_utc,omitempty"`
	Settings         *Config          `json:"search_settings,omitempty"`
	ModelInputs      []ModelInput     `json:"elevation_inputs,omitempty"`
	Error            string           `json:"error,omitempty"`
	Version          string           `json:"version"`
	Status           string           `json:"status"`
	Note             string           `json:"note"`
	Options          AutomaticOptions `json:"options"`
	Curves           []acoustic.Event `json:"curves"`
	ExcludedCurves   []CurveExclusion `json:"excluded_curves,omitempty"`
	Pairs            int              `json:"pairs"`
	Results          []GroupResult    `json:"group_results"`
	Best             *GroupResult     `json:"best,omitempty"`
	ControlMax       *float64         `json:"control_max_score_db,omitempty"`
	Margin           *float64         `json:"margin_db,omitempty"`
	Ambiguous        bool             `json:"ambiguous"`
}

type CurveExclusion struct {
	Index  int    `json:"event_index"`
	Reason string `json:"reason"`
}

// User-reviewed silence can contain an unrelated strong tonal proposal. In an
// automatic subset search that proposal is ineligible; it must not prevent the
// absence constraint from being applied to every actual aircraft hypothesis.
func ValidateAutomatic(c Config, s *acoustic.Spectrum) (float64, error) {
	check := c
	check.Silence = nil
	start, err := check.Validate(s)
	if err != nil {
		return 0, err
	}
	check = c
	check.Staged = false
	check.Events = nil
	return start, validateStaged(check, s)
}

func overlapsReviewedSilence(e acoustic.Event, quiet []SilentInterval) bool {
	for _, w := range quiet {
		if e.Start < w.End && e.End > w.Start {
			return true
		}
	}
	return false
}

// GroupCompleted receives each search's complete surface before it is released.
// Event indices refer to AutomaticReport.Curves, never to aircraft identities.
type GroupCompleted func(GroupResult, Config, Report) error

func RunAutomatic(ctx context.Context, c Config, s *acoustic.Spectrum, ts []tracks.Track, o AutomaticOptions, progress func(GroupProgress), completed GroupCompleted) (AutomaticReport, error) {
	started := time.Now().UTC().Format(time.RFC3339Nano)
	key, err := automaticFingerprint(ctx, s, ts)
	if err != nil {
		return AutomaticReport{}, err
	}
	models, err := automaticModelInputs(ctx, c, ts, o)
	if err != nil {
		return AutomaticReport{}, err
	}
	key = bindModelInputs(key, models)
	r, err := runAutomatic(ctx, c, s, ts, o, progress, completed, func(c Config, p func(string, int, int)) (Report, error) {
		return cachedAutomaticRun(ctx, o.CacheDir, key, c, func() (Report, error) { return RunTiled(ctx, c, s, ts, p) })
	})
	r.InputFingerprint, r.Started, r.Finished = key, started, time.Now().UTC().Format(time.RFC3339Nano)
	r.ModelInputs = models
	if err != nil {
		r.Error = err.Error()
	}
	return r, err
}

func runAutomatic(ctx context.Context, c Config, s *acoustic.Spectrum, ts []tracks.Track, o AutomaticOptions, progress func(GroupProgress), completed GroupCompleted, run func(Config, func(string, int, int)) (Report, error)) (AutomaticReport, error) {
	r := AutomaticReport{Version: "automatic-pairs-v1", Status: "incomplete", Options: o, Curves: c.Events, Results: []GroupResult{}, Note: "All eligible nonoverlapping pairs of strong acoustic curves are searched over the supplied region with one common clock and distinct flight tracks. Unselected curves are not declared aircraft or silence. Wrong-time trials rerun pair selection and location refinement. Passing these development controls is not calibrated confidence or verified GPS. Fixed-receiver and stable-source-tone assumptions apply; detection can miss aircraft. Track time-span checks do not prove receiver-network completeness."}
	if o.MaxPairs < 1 || o.MaxPairs > 4096 || !geo.Finite(o.Margin) || o.Margin < 0 || len(o.ControlShifts) > 19 {
		return r, fmt.Errorf("invalid automatic pair/control limits")
	}
	if len(c.Events) < 2 {
		return r, fmt.Errorf("automatic joint localization needs at least two strong acoustic curves")
	}
	var err error
	c, err = MeasuredJointConfig(c, c.Events)
	if err != nil {
		return r, err
	}
	c.CurveEnvelope = true
	r.Settings = &c
	if _, err = geo.NewMask(c.RegionMask); err != nil {
		return r, err
	}
	if _, err = ValidateAutomatic(c, s); err != nil {
		return r, err
	}
	var pairs [][2]int
	for i, a := range c.Events {
		if overlapsReviewedSilence(a, c.Silence) {
			r.ExcludedCurves = append(r.ExcludedCurves, CurveExclusion{i, "Overlaps a user-reviewed silence interval; cannot be selected as an observed flyover."})
			continue
		}
		for j := i + 1; j < len(c.Events); j++ {
			b := c.Events[j]
			if overlapsReviewedSilence(b, c.Silence) {
				continue
			}
			// Simultaneous ridges/harmonics are not separate flyover timing evidence.
			if a.End > b.Start {
				continue
			}
			pairs = append(pairs, [2]int{i, j})
		}
	}
	r.Pairs = len(pairs)
	if len(pairs) == 0 {
		return r, fmt.Errorf("no separate strong flyovers; overlapping tones cannot provide a flyover offset")
	}
	if len(pairs) > o.MaxPairs {
		return r, fmt.Errorf("%d eligible pairs exceed the explicit %d-pair budget; no pairs were silently dropped", len(pairs), o.MaxPairs)
	}
	seen := map[float64]bool{0: true}
	for _, shift := range o.ControlShifts {
		if !geo.Finite(shift) || seen[shift] || math.Abs(shift) <= 2*c.ClockUncertainty {
			return r, fmt.Errorf("control times must be distinct and outside the complete recording-clock uncertainty")
		}
		seen[shift] = true
	}
	t0, _ := time.Parse(time.RFC3339Nano, c.RecordingStart)
	shifts := append([]float64{0}, o.ControlShifts...)
	failed := false
	for trial, shift := range shifts {
		trialConfig := c
		trialConfig.RecordingStart = t0.Add(time.Duration(shift * float64(time.Second))).Format(time.RFC3339Nano)
		coverageErr := automaticTimeCoverage(trialConfig, ts)
		for k, pair := range pairs {
			if err = ctx.Err(); err != nil {
				return r, err
			}
			group := trialConfig
			group.Events = []acoustic.Event{c.Events[pair[0]], c.Events[pair[1]]}
			entry := GroupResult{Trial: trial, Pair: k, Events: pair, Shift: shift, Status: "error"}
			p := func(stage string, done, total int) {
				if progress != nil {
					progress(GroupProgress{trial + 1, len(shifts), k + 1, len(pairs), shift, stage, done, total})
				}
			}
			p("starting", 0, 0)
			var report Report
			if coverageErr != nil {
				err = coverageErr
			} else {
				report, err = run(group, p)
			}
			if err != nil {
				if ctx.Err() != nil {
					return r, ctx.Err()
				}
				entry.Error = err.Error()
				failed = true
			} else {
				entry.Status = report.Status
				entry.Locations = report.Evaluated
				entry.Ambiguous = automaticAmbiguity(report)
				if len(report.Candidates) > 0 {
					best := report.Candidates[0]
					entry.Best = &best
				}
				if completed != nil {
					if err = completed(entry, group, report); err != nil {
						return r, err
					}
				}
			}
			r.Results = append(r.Results, entry)
			p("finished", 1, 1)
		}
	}
	var actual []GroupResult
	for _, g := range r.Results {
		if g.Best == nil {
			continue
		}
		if g.Trial == 0 {
			actual = append(actual, g)
		} else if r.ControlMax == nil || g.Best.Score > *r.ControlMax {
			v := g.Best.Score
			r.ControlMax = &v
		}
	}
	sort.SliceStable(actual, func(i, j int) bool { return actual[i].Best.Score > actual[j].Best.Score })
	if len(actual) > 0 {
		best := actual[0]
		r.Best = &best
		r.Ambiguous = best.Ambiguous
		for _, g := range actual[1:] {
			if g.Best.Score < best.Best.Score-1 {
				continue
			}
			a, b := g.Best, best.Best
			if geo.ECEF(a.Lat, a.Lon, 0).Sub(geo.ECEF(b.Lat, b.Lon, 0)).Norm() > 1000 || math.Abs(a.ClockOffset-b.ClockOffset) > 5 {
				r.Ambiguous = true
			}
		}
	}
	switch {
	case failed:
		r.Status = "incomplete"
	case r.Best == nil:
		r.Status = "no_match"
	case len(o.ControlShifts) == 0:
		r.Status = "controls_not_run"
	default:
		if r.ControlMax != nil {
			margin := r.Best.Best.Score - *r.ControlMax
			r.Margin = &margin
		}
		if r.Ambiguous || r.Margin != nil && *r.Margin < o.Margin {
			r.Status = "unresolved"
		} else {
			r.Status = "candidate_sequence"
		}
	}
	return r, nil
}

// Inspect the complete surface: a close mirror solution can belong to the
// same aircraft pair and disappear when only the best flight group is kept.
func automaticAmbiguity(r Report) bool {
	if r.Ambiguity != "" {
		return true
	}
	if len(r.Candidates) == 0 {
		return false
	}
	b := r.Candidates[0]
	separated := func(lat, lon, clock, score float64) bool {
		return score >= b.Score-1 && (geo.ECEF(lat, lon, 0).Sub(geo.ECEF(b.Lat, b.Lon, 0)).Norm() > 1000 || math.Abs(clock-b.ClockOffset) > 5)
	}
	for _, p := range r.ScoreGrid {
		if p.MeetsThreshold && p.Score != nil && p.ClockOffset != nil && separated(p.Lat, p.Lon, *p.ClockOffset, *p.Score) {
			return true
		}
	}
	for _, p := range append(append([]Candidate(nil), r.Candidates...), r.FlightGroups...) {
		if separated(p.Lat, p.Lon, p.ClockOffset, p.Score) {
			return true
		}
	}
	return false
}

func automaticTimeCoverage(c Config, ts []tracks.Track) error {
	low, high := math.Inf(1), math.Inf(-1)
	for _, t := range ts {
		if len(t.Points) > 1 {
			low = math.Min(low, t.Points[0].Time)
			high = math.Max(high, t.Points[len(t.Points)-1].Time)
		}
	}
	t, _ := time.Parse(time.RFC3339Nano, c.RecordingStart)
	start := float64(t.Unix()) + float64(t.Nanosecond())/1e9
	a, b := math.Inf(1), math.Inf(-1)
	for _, e := range c.Events {
		a = math.Min(a, start+e.Start-c.ClockUncertainty-e.MaxSlant/c.SoundSpeed)
		b = math.Max(b, start+e.End+c.ClockUncertainty)
	}
	if low > a || high < b {
		return fmt.Errorf("flight history does not span this trial's full curve/clock window; extend telemetry before interpreting controls")
	}
	return nil
}
