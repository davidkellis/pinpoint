package search

import (
	"context"
	"crypto/sha256"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"

	"pinpoint/internal/acoustic"
	"pinpoint/internal/geo"
	"pinpoint/internal/tracks"
)

type SequenceEvent struct {
	Track     string  `json:"track_id"`
	Start     float64 `json:"start_s"`
	End       float64 `json:"end_s"`
	CPA       float64 `json:"predicted_audio_cpa_s"`
	F0        float64 `json:"f0_hz"`
	Train     float64 `json:"training_contrast_db"`
	Test      float64 `json:"heldout_contrast_db"`
	FlatTrain float64 `json:"steady_training_db"`
	FlatTest  float64 `json:"steady_heldout_db"`
}
type SequenceScore struct {
	Valid     bool            `json:"valid"`
	Quality   bool            `json:"passes_acoustic_checks"`
	Attempted int             `json:"encounters_compared"`
	Cohort    string          `json:"encounter_cohort_sha256"`
	Train     float64         `json:"weakest_training_margin_db"`
	Test      float64         `json:"weakest_heldout_margin_db"`
	Events    []SequenceEvent `json:"selected_additional_events"`
}
type SequenceControl struct {
	Shift float64       `json:"schedule_shift_s"`
	Score SequenceScore `json:"sequence"`
}
type SequenceCandidate struct {
	Seed     Candidate         `json:"seed"`
	Actual   SequenceScore     `json:"actual"`
	Controls []SequenceControl `json:"controls"`
	Paired   bool              `json:"complete_paired_controls"`
}
type SequenceMaximum struct {
	Shift     float64 `json:"schedule_shift_s"`
	Score     float64 `json:"best_heldout_margin_db"`
	Candidate int     `json:"candidate_index"`
}
type SequenceAudit struct {
	Note          string              `json:"note"`
	Conclusion    string              `json:"conclusion"`
	Extra         int                 `json:"additional_flights_required"`
	Shifts        []float64           `json:"schedule_shifts_s"`
	Candidates    []SequenceCandidate `json:"candidates"`
	Maxima        []SequenceMaximum   `json:"wrong_time_search_maxima"`
	Best          *int                `json:"best_candidate_index,omitempty"`
	ControlMargin *float64            `json:"margin_over_best_wrong_time_search_db,omitempty"`
	Separated     bool                `json:"separated_from_timing_controls"`
	Competing     bool                `json:"competing_sequences"`
}

func sequenceShifts(n int) []float64 {
	shifts := make([]float64, n)
	for i := range shifts {
		shifts[i] = 140 + 20*float64(i/2)
		if i%2 == 1 {
			shifts[i] = -shifts[i]
		}
	}
	return shifts
}

// Use the SAME encounter cohort for the real schedule and every null schedule.
// Both proposed window lengths must fit, with no seed audio in any trial. This
// avoids giving controls fewer opportunities at recording boundaries.
func sequenceEligible(c Config, s *acoustic.Spectrum, center float64, shifts []float64) bool {
	guard := float64(s.Window) / float64(s.SampleRate)
	for _, shift := range append([]float64{0}, shifts...) {
		from, to := center+shift-60, center+shift+60
		if from < s.Time(0) || to > s.Time(len(s.Rows)-1) || overlapsAnchors(c, from, to, guard) {
			return false
		}
	}
	return true
}

func selectSequence(c Config, s *acoustic.Spectrum, matches []GuidedMatch, o GuidedOptions, shifts []float64, shift float64) SequenceScore {
	r := SequenceScore{Events: []SequenceEvent{}}
	var options []GuidedMatch
	for _, m := range matches {
		if !m.Fit.Valid || !m.Flat.Valid || !sequenceEligible(c, s, m.CPA-shift, shifts) {
			continue
		}
		options = append(options, m)
	}
	r.Attempted = len(options)
	var cohort []string
	for _, m := range options {
		cohort = append(cohort, fmt.Sprintf("%s/%.3f", m.Track, m.CPA-shift))
	}
	sort.Strings(cohort)
	r.Cohort = fmt.Sprintf("%x", sha256.Sum256([]byte(strings.Join(cohort, "\n"))))
	// Selection never reads heldout scores, local-control outcomes or Supported.
	utility := func(m GuidedMatch) float64 { return m.Fit.Train - math.Max(0, m.Flat.Train) }
	sort.SliceStable(options, func(i, j int) bool {
		a, b := utility(options[i]), utility(options[j])
		if a != b {
			return a > b
		}
		if options[i].Track != options[j].Track {
			return options[i].Track < options[j].Track
		}
		return options[i].Start < options[j].Start
	})
	var chosen []GuidedMatch
	guard := float64(s.Window) / float64(s.SampleRate)
	for _, m := range options {
		duplicate := false
		for _, q := range chosen {
			if guidedAircraftID(m.Track) == guidedAircraftID(q.Track) || m.Start < q.End+guard && m.End > q.Start-guard {
				duplicate = true
				break
			}
		}
		if duplicate {
			continue
		}
		chosen = append(chosen, m)
		if len(chosen) == o.SequenceExtra {
			break
		}
	}
	if len(chosen) < o.SequenceExtra {
		return r
	}
	r.Valid, r.Quality = true, true
	r.Train, r.Test = math.Inf(1), math.Inf(1)
	sort.Slice(chosen, func(i, j int) bool { return chosen[i].CPA < chosen[j].CPA })
	for _, m := range chosen {
		lo, hi := math.Inf(1), 0.
		for _, p := range m.Curve {
			lo = math.Min(lo, p.Hz)
			hi = math.Max(hi, p.Hz)
		}
		r.Train = math.Min(r.Train, utility(m))
		r.Test = math.Min(r.Test, m.Fit.Heldout-math.Max(0, m.Flat.Heldout))
		r.Quality = r.Quality && hi/lo >= 1.03 && m.Fit.Train >= o.MinContrast && m.Fit.Heldout >= o.MinContrast && m.Fit.Positive >= .65
		r.Events = append(r.Events, SequenceEvent{m.Track, m.Start, m.End, m.CPA, m.Fit.F0, m.Fit.Train, m.Fit.Heldout, m.Flat.Train, m.Flat.Heldout})
	}
	return r
}

func auditSequences(ctx context.Context, c Config, s *acoustic.Spectrum, ts []tracks.Track, seeds []GuidedCandidate, epoch float64, o GuidedOptions, progress func(int, int)) (SequenceAudit, error) {
	r := SequenceAudit{Extra: o.SequenceExtra, Shifts: sequenceShifts(o.SequenceControls), Candidates: []SequenceCandidate{}, Maxima: []SequenceMaximum{}, Conclusion: "insufficient_paired_sequences", Note: "Experimental conditional timing control, not a probability or independent validation. Anchor position/clock stay fixed. Each sequence is selected on training audio only; its weakest additional-flight test margin is scored. Every wrong-time trial reruns tone, window and flight selection over the same eligible encounters, then takes the maximum across all eligible candidate locations. The anchor is not shifted; all additional flight times are shifted together. Correlated blocks/shifts, anchor bias, finite seed coverage and unmodeled sound remain limitations. Missing support is not silence."}
	for _, p := range seeds {
		if !p.AnchorValid {
			continue
		}
		r.Candidates = append(r.Candidates, SequenceCandidate{Seed: p.Seed, Actual: selectSequence(c, s, p.Matches, o, r.Shifts, 0), Controls: make([]SequenceControl, len(r.Shifts))})
	}
	type task struct{ seed, trial int }
	type result struct {
		task
		score SequenceScore
		err   error
	}
	jobs := make(chan task)
	done := make(chan result, o.Workers)
	var wg sync.WaitGroup
	for i := 0; i < o.Workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				p, err := guidedSeedAt(ctx, c, s, ts, r.Candidates[job.seed].Seed, epoch, o, r.Shifts[job.trial], r.Shifts, false)
				var score SequenceScore
				if err == nil && p.AnchorValid {
					score = selectSequence(c, s, p.Matches, o, r.Shifts, r.Shifts[job.trial])
				}
				done <- result{job, score, err}
			}
		}()
	}
	go func() {
		defer close(jobs)
		for i := range r.Candidates {
			for j := range r.Shifts {
				select {
				case jobs <- task{i, j}:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	go func() { wg.Wait(); close(done) }()
	var failure error
	completed := 0
	for v := range done {
		r.Candidates[v.seed].Controls[v.trial] = SequenceControl{r.Shifts[v.trial], v.score}
		if v.err != nil && failure == nil {
			failure = v.err
		}
		completed++
		if progress != nil {
			progress(completed, len(r.Candidates)*len(r.Shifts))
		}
	}
	if err := ctx.Err(); err != nil {
		return r, err
	}
	if failure != nil {
		return r, failure
	}
	finishSequenceAudit(&r, o)
	return r, nil
}

func finishSequenceAudit(r *SequenceAudit, o GuidedOptions) {
	var paired []int
	for i := range r.Candidates {
		p := &r.Candidates[i]
		p.Paired = p.Actual.Valid && len(p.Controls) == len(r.Shifts)
		for _, control := range p.Controls {
			p.Paired = p.Paired && control.Score.Valid && control.Score.Cohort == p.Actual.Cohort
		}
		if p.Paired {
			paired = append(paired, i)
		}
	}
	if len(paired) == 0 {
		return
	}
	best := paired[0]
	for _, i := range paired {
		if r.Candidates[i].Actual.Test > r.Candidates[best].Actual.Test {
			best = i
		}
	}
	r.Best = &best
	strongest := math.Inf(-1)
	for j, shift := range r.Shifts {
		maxi := paired[0]
		for _, i := range paired {
			if r.Candidates[i].Controls[j].Score.Test > r.Candidates[maxi].Controls[j].Score.Test {
				maxi = i
			}
		}
		score := r.Candidates[maxi].Controls[j].Score.Test
		r.Maxima = append(r.Maxima, SequenceMaximum{shift, score, maxi})
		strongest = math.Max(strongest, score)
	}
	p := r.Candidates[best]
	margin := p.Actual.Test - strongest
	r.ControlMargin = &margin
	r.Separated = p.Actual.Quality && p.Actual.Test >= o.ControlMargin && margin >= o.ControlMargin
	key := func(p SequenceCandidate) string {
		var ids []string
		for _, e := range p.Actual.Events {
			ids = append(ids, e.Track)
		}
		return flightKey(p.Seed) + "/" + strings.Join(ids, "/")
	}
	for _, i := range paired {
		if i == best {
			continue
		}
		q := r.Candidates[i]
		if q.Actual.Quality && p.Actual.Test-q.Actual.Test <= o.ControlMargin && (key(p) != key(q) || geo.ECEF(p.Seed.Lat, p.Seed.Lon, p.Seed.Height).Sub(geo.ECEF(q.Seed.Lat, q.Seed.Lon, q.Seed.Height)).Norm() > 1000) {
			r.Competing = true
		}
	}
	switch {
	case !p.Actual.Quality:
		r.Conclusion = "no_acoustic_support"
	case !r.Separated:
		r.Conclusion = "not_separated_from_wrong_timing"
	case r.Competing:
		r.Conclusion = "competing_sequences"
	default:
		r.Conclusion = "passes_timing_controls_unverified"
	}
}
