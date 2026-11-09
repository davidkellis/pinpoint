package search

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"pinpoint/internal/acoustic"
	"pinpoint/internal/geo"
	"pinpoint/internal/tracks"
	"sort"
	"strconv"
	"strings"
	"time"
)

func RunAutomaticSequences(ctx context.Context, c Config, s *acoustic.Spectrum, ts []tracks.Track, o SequenceOptions, progress func(SequenceProgress), completed SequenceCompleted) (r SequenceReport, err error) {
	started := time.Now().UTC().Format(time.RFC3339Nano)
	defer func() {
		if r.Version == "" {
			r = SequenceReport{Version: "automatic-sequences-v2", Status: "incomplete", Options: o, Curves: c.Events, Settings: &c}
		}
		r.Started, r.Finished = started, time.Now().UTC().Format(time.RFC3339Nano)
		if err != nil {
			r.Error = err.Error()
		}
	}()
	base, err := automaticFingerprint(ctx, s, ts)
	if err != nil {
		return SequenceReport{}, err
	}
	audioSHA, err := sequenceAudioDigest(ctx, o.AudioPath)
	if err != nil {
		return SequenceReport{}, err
	}
	models, err := automaticModelInputs(ctx, c, ts, AutomaticOptions{ControlShifts: o.ControlShifts})
	if err != nil {
		return SequenceReport{}, err
	}
	key := bindModelInputs(base, models)
	sum := sha256.Sum256([]byte(key + audioSHA))
	key = hex.EncodeToString(sum[:])
	if err = validateSequenceOptions(c, o); err != nil {
		return SequenceReport{}, err
	}
	bank, paths, err := sequenceSpectra(ctx, c, o, key, progress)
	if err != nil {
		return SequenceReport{}, err
	}
	c, err = trainSequenceConfig(c, bank, paths)
	if err != nil {
		return SequenceReport{}, err
	}
	r, err = runSequenceSearch(ctx, c, s, ts, o, progress, completed, func(q Config, p func(string, int, int)) (Report, error) {
		return cachedAutomaticRun(ctx, o.CacheDir, key, q, func() (Report, error) { return RunTiled(ctx, q, s, ts, p) })
	})
	r.AudioSHA, r.Fingerprint, r.ModelInputs = audioSHA, key, models
	return r, err
}

func validateSequenceOptions(c Config, o SequenceOptions) error {
	if o.Workers < 1 || o.Workers > 8 || o.Channel < 0 || o.MaxGroups < 1 || o.MaxGroups > 10000 || !geo.Finite(o.Margin) || o.Margin < 0 || len(o.ControlShifts) > 39 {
		return fmt.Errorf("invalid automatic sequence limits")
	}
	seen := map[float64]bool{0: true}
	for _, v := range o.ControlShifts {
		if !geo.Finite(v) || seen[v] || math.Abs(v) <= 2*c.ClockUncertainty {
			return fmt.Errorf("wrong-time trials must be distinct and outside the entire clock prior")
		}
		seen[v] = true
	}
	return nil
}

func runSequenceSearch(ctx context.Context, c Config, s *acoustic.Spectrum, ts []tracks.Track, o SequenceOptions, progress func(SequenceProgress), completed SequenceCompleted, run func(Config, func(string, int, int)) (Report, error)) (SequenceReport, error) {
	r := SequenceReport{Version: "automatic-sequences-v2", Status: "incomplete", Options: o, Curves: c.Events, Groups: []SequenceGroup{}, Reasons: []Rejection{}, Note: "Proposal-conditioned validation: acoustic proposals were discovered on the recording; training-only fitting and training-derived spectral normalization reserve guarded blocks for checking frozen flight assignments, location, clock and source tones. Every larger group is searched over the original region only if all smaller subsets pass training. Controls repeat the full group search. The weakest reserved-event margin is tested against steady/linear tones and the worst wrong-time group. Correlated samples, proposal selection, finite grids/refinement, stable source pitch, still air and telemetry coverage remain limitations. Scores and control separation are not calibrated GPS confidence. Unassigned sounds are not aircraft or silence."}
	if err := validateSequenceOptions(c, o); err != nil {
		return r, err
	}
	var err error
	c, err = MeasuredJointConfig(c, c.Events)
	if err != nil {
		return r, err
	}
	r.Settings = &c
	if _, err = ValidateAutomatic(c, s); err != nil {
		return r, err
	}
	if _, err = geo.NewMask(c.RegionMask); err != nil {
		return r, err
	}
	for i, e := range c.Events {
		if overlapsReviewedSilence(e, c.Silence) {
			r.Excluded = append(r.Excluded, CurveExclusion{i, "Overlaps user-reviewed silence; not eligible as an aircraft observation."})
		}
	}
	base, _ := time.Parse(time.RFC3339Nano, c.RecordingStart)
	failed := false
	shifts := append([]float64{0}, o.ControlShifts...)
	for trial, shift := range shifts {
		q := c
		q.RecordingStart = base.Add(time.Duration(shift * float64(time.Second))).Format(time.RFC3339Nano)
		if err := automaticTimeCoverage(q, ts); err != nil {
			r.Groups = append(r.Groups, SequenceGroup{ID: len(r.Groups), Trial: trial, Shift: shift, Status: "error", Reasons: []Rejection{{Code: "telemetry_window", Detail: err.Error()}}})
			failed = true
			continue
		}
		groups := sequencePairs(q.Events, q.Silence)
		searched := 0
		for size := 2; len(groups) > 0; size++ {
			if searched+len(groups) > o.MaxGroups {
				r.Reasons = append(r.Reasons, Rejection{Code: "group_budget", Detail: fmt.Sprintf("Trial %d would exceed %d groups; no groups were silently dropped.", trial+1, o.MaxGroups)})
				failed = true
				break
			}
			passed := map[string][]int{}
			for k, indices := range groups {
				if err = ctx.Err(); err != nil {
					return r, err
				}
				group := subsetSequenceConfig(q, indices)
				p := func(stage string, done, total int) {
					if progress != nil {
						progress(SequenceProgress{trial + 1, len(shifts), size, k + 1, len(groups), shift, stage, done, total})
					}
				}
				p("starting", 0, 0)
				entry := SequenceGroup{ID: len(r.Groups), Trial: trial, Shift: shift, Indices: append([]int(nil), indices...), Status: "no_training_match", Reasons: []Rejection{}}
				report, runErr := run(group, p)
				searched++
				if runErr != nil {
					if ctx.Err() != nil {
						return r, ctx.Err()
					}
					entry.Status = "error"
					entry.Reasons = append(entry.Reasons, Rejection{Code: "search_failed", Detail: runErr.Error()})
					failed = true
				} else {
					entry.Locations = report.Evaluated
					if len(report.Candidates) == 0 {
						entry.Reasons = append(entry.Reasons, Rejection{Code: "no_training_match", Detail: "No evaluated location/clock passes every event's training geometry, ridge and signal checks, or reviewed silence."})
						var codes []string
						for code := range report.Rejections {
							codes = append(codes, code)
						}
						sort.Strings(codes)
						for _, code := range codes {
							entry.Reasons = append(entry.Reasons, Rejection{Code: code, Detail: searchRejectionDetail(code), Count: report.Rejections[code]})
						}
					} else {
						b := report.Candidates[0]
						entry.Best = &b
						entry.TrainingAmbiguous = automaticAmbiguity(report)
						passed[indexKey(indices)] = indices
						entry.Checks, entry.Validation, entry.Pass = fixedSequenceChecks(group, s, ts, b, indices)
						entry.Status = "validation_failed"
						if entry.Pass {
							entry.Status = "validation_pass"
						} else {
							entry.Reasons = append(entry.Reasons, Rejection{Code: "reserved_audio_failure", Detail: "At least one selected aircraft fails the frozen-parameter reserved-audio checks; no event was dropped or retimed to repair it."})
						}
						group.validationChecks = entry.Checks
						group.validationCandidate = entry.Best
					}
					if completed != nil {
						if err = completed(entry, group, report); err != nil {
							return r, err
						}
					}
				}
				r.Groups = append(r.Groups, entry)
				p("finished", 1, 1)
			}
			// Expansion reads TRAINING outcomes only, never validation results.
			groups = nextSequenceGroups(passed, q.Events, q.Silence)
		}
	}
	finishSequenceSearch(&r, failed)
	return r, nil
}

func sequencePairs(events []acoustic.Event, quiet []SilentInterval) [][]int {
	var out [][]int
	for i, a := range events {
		if overlapsReviewedSilence(a, quiet) {
			continue
		}
		for j := i + 1; j < len(events); j++ {
			if a.End <= events[j].Start && !overlapsReviewedSilence(events[j], quiet) {
				out = append(out, []int{i, j})
			}
		}
	}
	return out
}
func indexKey(indices []int) string {
	var b strings.Builder
	for _, i := range indices {
		b.WriteString(strconv.Itoa(i))
		b.WriteByte(',')
	}
	return b.String()
}
func nextSequenceGroups(passed map[string][]int, events []acoustic.Event, quiet []SilentInterval) [][]int {
	proposals := map[string][]int{}
	for _, group := range passed {
		for i := group[len(group)-1] + 1; i < len(events); i++ {
			if events[group[len(group)-1]].End > events[i].Start || overlapsReviewedSilence(events[i], quiet) {
				continue
			}
			v := append(append([]int(nil), group...), i)
			all := true
			for skip := range v {
				parent := append(append([]int(nil), v[:skip]...), v[skip+1:]...)
				if _, ok := passed[indexKey(parent)]; !ok {
					all = false
					break
				}
			}
			if all {
				proposals[indexKey(v)] = v
			}
		}
	}
	var out [][]int
	for _, v := range proposals {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool {
		for k := range out[i] {
			if out[i][k] != out[j][k] {
				return out[i][k] < out[j][k]
			}
		}
		return false
	})
	return out
}
func betterSequenceTraining(a, b SequenceGroup) bool {
	if len(a.Indices) != len(b.Indices) {
		return len(a.Indices) > len(b.Indices)
	}
	if a.Best.Score != b.Best.Score {
		return a.Best.Score > b.Best.Score
	}
	return a.ID < b.ID
}
func finishSequenceSearch(r *SequenceReport, failed bool) {
	for i, g := range r.Groups {
		if g.Trial == 0 && g.Best != nil && (r.Selected == nil || betterSequenceTraining(g, r.Groups[*r.Selected])) {
			v := i
			r.Selected = &v
		}
		// The null gets the conservative maximum over every group's frozen
		// training winner, including groups that fail a separate quality gate.
		if g.Trial > 0 && g.Validation != nil && (r.ControlMax == nil || *g.Validation > *r.ControlMax) {
			v := *g.Validation
			r.ControlMax = &v
		}
	}
	switch {
	case failed:
		r.Status = "incomplete"
	case r.Selected == nil:
		r.Status = "no_match"
	case len(r.Options.ControlShifts) == 0:
		r.Status = "controls_not_run"
	default:
		best := r.Groups[*r.Selected]
		ambiguous := best.TrainingAmbiguous
		for _, g := range r.Groups {
			if g.Trial != 0 || g.Best == nil || len(g.Indices) != len(best.Indices) || best.Best.Score-g.Best.Score > 1 {
				continue
			}
			if geo.ECEF(g.Best.Lat, g.Best.Lon, 0).Sub(geo.ECEF(best.Best.Lat, best.Best.Lon, 0)).Norm() > 1000 || math.Abs(g.Best.ClockOffset-best.Best.ClockOffset) > 5 {
				ambiguous = true
			}
		}
		if best.Validation != nil && r.ControlMax != nil {
			v := *best.Validation - *r.ControlMax
			r.Margin = &v
		}
		r.Status = "candidate_sequence"
		if !best.Pass {
			r.Status = "unresolved"
			r.Reasons = append(r.Reasons, Rejection{Code: "reserved_audio_failure", Detail: "The group selected on training evidence does not pass reserved-audio checks."})
		}
		if ambiguous {
			r.Status = "unresolved"
			r.Reasons = append(r.Reasons, Rejection{Code: "competing_training_hypotheses", Detail: "A distant location or different shared clock remains competitive on training evidence."})
		}
		if r.Margin != nil && *r.Margin < r.Options.Margin {
			r.Status = "unresolved"
			r.Reasons = append(r.Reasons, Rejection{Code: "wrong_time_competitor", Detail: fmt.Sprintf("Reserved-audio separation %.2f dB is below %.2f dB.", *r.Margin, r.Options.Margin)})
		}
	}
}

func searchRejectionDetail(code string) string {
	switch code {
	case "no_eligible_trajectory":
		return "No supplied trajectory meets the time, speed and distance requirements at these evaluated points."
	case "clock_envelope":
		return "The common clock is excluded by the coarse curve envelope."
	case "reviewed_silence":
		return "Every evaluated common clock predicts aircraft within a user-reviewed quiet interval and audibility radius."
	case "relative_arrival_timing":
		return "The event arrival times cannot share one recording clock."
	case "aircraft_identity_conflict":
		return "Individual tones fit, but they cannot be assigned to distinct aircraft."
	case "weak_spectral_signal":
		return "Complete models remain below at least one event's training signal threshold."
	case "curve_shape_or_coverage":
		return "No model supplies adequate received-sound coverage and measured-curve agreement."
	case "coarse_geometry_or_curve_envelope":
		return "Coarse cells lack a compatible joint arrival/curve interval."
	default:
		return "Training hypothesis rejected."
	}
}
