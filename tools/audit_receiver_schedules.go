//go:build ignore

// Compare whole flight schedules at a disclosed receiver, without an anchor.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"pinpoint/internal/acoustic"
	"pinpoint/internal/geo"
	"pinpoint/internal/search"
	"pinpoint/internal/tracks"
)

type scheduleTrial struct {
	Shift   float64              `json:"shared_clock_shift_s"`
	Cohort  int                  `json:"matched_encounter_opportunities"`
	Valid   bool                 `json:"enough_distinct_flights"`
	Quality bool                 `json:"all_selected_flights_pass_checks"`
	Train   float64              `json:"weakest_training_margin_db"`
	Test    float64              `json:"weakest_reserved_margin_db"`
	Events  []search.GuidedMatch `json:"training_selected_events"`
}

func required(e error) {
	if e != nil {
		panic(e)
	}
}
func saveAudit(p string, v any) {
	b, e := json.MarshalIndent(v, "", "  ")
	required(e)
	required(os.WriteFile(p, append(b, '\n'), 0600))
}
func airframe(id string) string {
	parts := strings.Split(id, "-")
	if len(parts) == 3 && len(parts[0]) == 6 {
		return strings.ToLower(parts[0])
	}
	return id
}
func cohortKey(m search.GuidedMatch, shift float64) string {
	return fmt.Sprintf("%s/%.3f", m.Track, m.CPA+shift)
}
func chooseSchedule(r search.ReceiverReview, shift float64, cohort map[string]bool, extra int) scheduleTrial {
	result := scheduleTrial{Shift: shift, Events: []search.GuidedMatch{}}
	var candidates []search.GuidedMatch
	for _, m := range r.Matches {
		if cohort[cohortKey(m, shift)] {
			candidates = append(candidates, m)
		}
	}
	result.Cohort = len(candidates)
	utility := func(m search.GuidedMatch) float64 { return m.Fit.Train - math.Max(0, m.Flat.Train) }
	sort.SliceStable(candidates, func(i, j int) bool {
		if utility(candidates[i]) != utility(candidates[j]) {
			return utility(candidates[i]) > utility(candidates[j])
		}
		return candidates[i].Track < candidates[j].Track
	})
	train, test, quality := math.Inf(1), math.Inf(1), true
	for _, m := range candidates {
		conflict := false
		for _, p := range result.Events {
			if airframe(m.Track) == airframe(p.Track) || m.Start < p.End+1 && m.End > p.Start-1 {
				conflict = true
				break
			}
		}
		if conflict {
			continue
		}
		train = math.Min(train, utility(m))
		test = math.Min(test, m.Fit.Heldout-math.Max(0, m.Flat.Heldout))
		quality = quality && m.Supported
		m.Curve = nil
		m.Controls = nil
		m.Alternatives = nil
		result.Events = append(result.Events, m)
		if len(result.Events) == extra {
			break
		}
	}
	if len(result.Events) == extra {
		result.Valid = true
		result.Quality = quality
		result.Train = train
		result.Test = test
	}
	return result
}

func main() {
	for _, a := range os.Args[1:] {
		if strings.HasPrefix(a, "-") && !strings.HasPrefix(a, "--") {
			panic("use two-hyphen options")
		}
	}
	config := flag.String("config", "", "disclosed receiver and recordings")
	spectra := flag.String("spectra-root", "", "root containing recording/receiver-short-spectrum.bin")
	out := flag.String("out", "work/receiver-schedule-audit", "new output directory")
	trials := flag.Int("controls", 7, "whole-schedule shifts, 5..19")
	flights := flag.Int("flights", 2, "distinct nonoverlapping aircraft required, 2..4")
	sourceStep := flag.Float64("source-step", 2, "source frequency sweep step in Hz")
	smoothStep := flag.Float64("smooth-track-step", 0, "optional Hermite interpolation step, 0 disables")
	reported := flag.Bool("reported-motion", false, "use retained ADS-B motion in Hermite interpolation")
	flag.Parse()
	if !geo.Finite(*sourceStep) || *sourceStep <= 0 || !geo.Finite(*smoothStep) || *smoothStep != 0 && (*smoothStep < .05 || *smoothStep > 2) || *reported && *smoothStep == 0 {
		panic("invalid motion/source limits")
	}
	if *config == "" || *spectra == "" || *trials < 5 || *trials > 19 || *flights < 2 || *flights > 4 {
		panic("invalid schedule audit limits")
	}
	b, e := os.ReadFile(*config)
	required(e)
	var cfg struct {
		Lat        float64                              `json:"lat"`
		Lon        float64                              `json:"lon"`
		Terrain    string                               `json:"terrain_dir"`
		Geoid      string                               `json:"geoid_file"`
		Recordings []struct{ ID, Start, Tracks string } `json:"recordings"`
	}
	required(json.Unmarshal(b, &cfg))
	terrain := geo.Terrain{Dir: cfg.Terrain}
	ground, e := terrain.Height(cfg.Lat, cfg.Lon)
	required(e)
	geoid, e := geo.LoadGeoid(cfg.Geoid)
	required(e)
	und, e := geoid.Height(cfg.Lat, cfg.Lon)
	required(e)
	shifts := []float64{0}
	for i := 0; i < *trials; i++ {
		v := 140 + 20*float64(i/2)
		if i%2 == 1 {
			v = -v
		}
		shifts = append(shifts, v)
	}
	maxShift := 0.
	for _, v := range shifts {
		maxShift = math.Max(maxShift, math.Abs(v))
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	required(os.MkdirAll(*out, 0700))
	var summaries []any
	for _, r := range cfg.Recordings {
		if filepath.Base(r.ID) != r.ID {
			panic("invalid recording ID")
		}
		s, e := acoustic.Load(filepath.Join(*spectra, r.ID, "receiver-short-spectrum.bin"))
		required(e)
		f, e := os.Open(r.Tracks)
		required(e)
		ts, e := tracks.Read(f)
		f.Close()
		required(e)
		o := search.DefaultGuidedOptions()
		o.StepHz = *sourceStep
		o.MaxHz = math.Min(6000, 2*s.Hz(len(s.Rows[0])-1))
		var smoothing []tracks.SmoothingReport
		if *smoothStep > 0 {
			n := 0
			for i := range ts {
				v, r, e := ts[i].Smooth(tracks.SmoothingOptions{Step: *smoothStep, MaxGap: 30, MaxAcceleration: 20, UseKinematics: *reported})
				required(e)
				ts[i] = v
				smoothing = append(smoothing, r)
				n += r.ReportedHorizontal
			}
			if *reported && n == 0 {
				panic("no reported horizontal motion")
			}
		}
		origin := 0.
		o.FoldOrigin = &origin
		var reports []search.ReceiverReview
		cohort := map[string]bool{}
		for index, shift := range shifts {
			h := search.ReceiverHypothesis{Lat: cfg.Lat, Lon: cfg.Lon, Height: ground + und + 1.5, RecordingStart: r.Start, Clock: shift, SoundSpeed: 343, MaxGap: 30}
			review, e := search.ReviewReceiver(ctx, h, s, ts, o)
			required(e)
			present := map[string]bool{}
			for _, m := range review.Matches {
				center := m.CPA + shift
				if m.Fit.Valid && m.Flat.Valid && center-maxShift-60 >= s.Time(0) && center+maxShift+60 <= s.Time(len(s.Rows)-1) {
					present[cohortKey(m, shift)] = true
				}
			}
			if index == 0 {
				cohort = present
			} else {
				for key := range cohort {
					if !present[key] {
						delete(cohort, key)
					}
				}
			}
			reports = append(reports, review)
			fmt.Printf("%s: complete shared-clock trial %d/%d\n", r.ID, index+1, len(shifts))
		}
		var scores []scheduleTrial
		for i, review := range reports {
			scores = append(scores, chooseSchedule(review, shifts[i], cohort, *flights))
		}
		status := "insufficient_paired_flights"
		margin := 0.
		if scores[0].Valid {
			status = "fails_schedule_controls"
			best := math.Inf(-1)
			complete := true
			for _, s := range scores[1:] {
				if !s.Valid {
					complete = false
				}
				best = math.Max(best, s.Test)
			}
			if complete {
				margin = scores[0].Test - best
				if scores[0].Quality && margin >= 1 {
					status = "experimental_schedule_separation"
				}
			}
		}
		sortKeys := []string{}
		for key := range cohort {
			sortKeys = append(sortKeys, key)
		}
		sort.Strings(sortKeys)
		result := map[string]any{"source_step_hz": o.StepHz, "max_source_hz": o.MaxHz, "smooth_track_step_s": *smoothStep, "reported_motion": *reported, "smoothing": smoothing, "recording": r.ID, "supplied_receiver": []float64{cfg.Lat, cfg.Lon}, "status": status, "required_distinct_aircraft": *flights, "matched_encounter_cohort": sortKeys, "trials": scores, "margin_over_best_wrong_schedule_db": margin, "fixed_fold_origin_audio_s": 0, "note": "Known-receiver experiment. Each schedule moves ALL flights by one shared clock shift; relative flyover offsets stay fixed. Each trial reselects tone/window/distinct flights on training evidence only. All trials compare the same eligible encounter cohort and recording-wide folds. Selected failures cannot be rescued by another heldout winner. This does not include receiver-position search, calibrated false-positive probability, aircraft identity truth or blind localization. Correlated controls and source/receiver model limits remain."}
		dir := filepath.Join(*out, r.ID)
		required(os.MkdirAll(dir, 0700))
		saveAudit(filepath.Join(dir, "schedule.json"), result)
		summaries = append(summaries, map[string]any{"recording": r.ID, "status": status, "matched_encounters": len(cohort), "actual_weakest_reserved_margin_db": scores[0].Test, "margin_over_best_wrong_schedule_db": margin})
		fmt.Printf("%s: %s; %d matched encounters; margin %.2f dB\n", r.ID, status, len(cohort), margin)
	}
	saveAudit(filepath.Join(*out, "summary.json"), map[string]any{"recordings": summaries, "controls": *trials, "required_distinct_aircraft": *flights, "created_utc": time.Now().UTC(), "note": "Supplied-receiver timing study, not blind GPS or independently verified acoustic identity."})
}
