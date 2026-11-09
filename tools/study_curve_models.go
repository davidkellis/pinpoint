//go:build ignore

// Compare bounded source-pitch and receiver-motion models on existing ridges.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"html"
	"math"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"pinpoint/internal/acoustic"
	"pinpoint/internal/geo"
	"pinpoint/internal/search"
	"pinpoint/internal/tracks"
)

type recording struct{ ID, Audio, Start, Tracks, Analysis string }
type studyConfig struct {
	Lat        float64     `json:"lat"`
	Lon        float64     `json:"lon"`
	Terrain    string      `json:"terrain_dir"`
	Geoid      string      `json:"geoid_file"`
	Recordings []recording `json:"recordings"`
}
type fitRow struct {
	Event        int                  `json:"event"`
	Level        string               `json:"original_evidence_level"`
	Start        float64              `json:"start_s"`
	End          float64              `json:"end_s"`
	Track        string               `json:"track_id"`
	Receiver     string               `json:"receiver_label"`
	Path         search.CurveReceiver `json:"receiver_path"`
	Fit          search.CurveModelFit `json:"fit"`
	TrainingGain float64              `json:"training_gain_over_constant_and_linear"`
}
type schedule struct {
	Receiver     string               `json:"receiver_label"`
	Path         search.CurveReceiver `json:"shared_receiver_path_and_clock"`
	Events       []fitRow             `json:"training_selected_nonoverlapping_distinct_aircraft"`
	Weakest      float64              `json:"weakest_training_gain"`
	ReservedPass bool                 `json:"all_reserved_shapes_agree"`
	Selected     bool                 `json:"selected_on_training"`
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
func save(path string, v any) {
	b, e := json.MarshalIndent(v, "", "  ")
	must(e)
	must(os.WriteFile(path, append(b, '\n'), 0600))
}
func sha(path string) string {
	b, e := os.ReadFile(path)
	must(e)
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}
func aircraft(id string) string {
	v := strings.Split(id, "-")
	if len(v) == 3 && len(v[0]) == 6 {
		return strings.ToLower(v[0])
	}
	return id
}
func utility(f search.CurveModelFit) float64 {
	b := math.Min(f.TrainConstant, f.TrainLinear)
	return 1 - f.Training*f.Training/math.Max(.01, b*b)
}
func selected(r search.CurveModelReview) (search.CurveModelFit, bool) {
	for _, f := range r.Fits {
		if f.Selected && f.Coverage >= .999999 {
			return f, true
		}
	}
	return search.CurveModelFit{}, false
}

func main() {
	for _, arg := range os.Args[1:] {
		if strings.HasPrefix(arg, "-") && !strings.HasPrefix(arg, "--") {
			panic("use two-hyphen options")
		}
	}
	config := flag.String("config", "", "receiver and recording configuration")
	out := flag.String("out", "work/curve-model-study", "new experimental output directory")
	compare := flag.String("compare-receiver", "", "optional alternative lat,lon, for disclosed-location diagnostics")
	drift := flag.Float64("max-source-change", .05, "maximum fractional source-pitch change within one curve, 0..0.25")
	motion := flag.Float64("motion-speed", 0, "also compare eight constant horizontal velocity directions at this speed, 0..5 m/s")
	clockWindow := flag.Float64("clock-window", 6, "shared clock grid half width, seconds")
	clockStep := flag.Float64("clock-step", 2, "shared clock step, seconds")
	includeReview := flag.Bool("include-review", false, "inspect review-tier proposals without promoting their labels")
	smoothStep := flag.Float64("smooth-track-step", 0, "experimental Hermite telemetry interpolation step; 0 disables, 0.05..2 seconds")
	flag.Parse()
	if *config == "" || *motion < 0 || *motion > 5 || *clockWindow < 0 || *clockWindow > 60 || *clockStep <= 0 || 2**clockWindow / *clockStep > 120 || !geo.Finite(*motion) || !geo.Finite(*clockWindow) || !geo.Finite(*clockStep) || !geo.Finite(*drift) || *drift < 0 || *drift > .25 {
		panic("invalid study limits")
	}
	if !geo.Finite(*smoothStep) || *smoothStep != 0 && (*smoothStep < .05 || *smoothStep > 2) {
		panic("invalid smoothing step")
	}
	b, e := os.ReadFile(*config)
	must(e)
	var cfg studyConfig
	must(json.Unmarshal(b, &cfg))
	if len(cfg.Recordings) == 0 {
		panic("no recordings")
	}
	type receiver struct {
		label            string
		lat, lon, height float64
	}
	receivers := []receiver{{label: "configured_receiver", lat: cfg.Lat, lon: cfg.Lon}}
	if *compare != "" {
		parts := strings.Split(*compare, ",")
		if len(parts) != 2 {
			panic("compare-receiver must be lat,lon")
		}
		lat, e := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
		must(e)
		lon, e := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
		must(e)
		receivers = append(receivers, receiver{label: "alternative_receiver", lat: lat, lon: lon})
	}
	terrain := geo.Terrain{Dir: cfg.Terrain}
	geoid, e := geo.LoadGeoid(cfg.Geoid)
	must(e)
	for i := range receivers {
		if !geo.Finite(receivers[i].lat) || !geo.Finite(receivers[i].lon) || math.Abs(receivers[i].lat) > 85 || math.Abs(receivers[i].lon) > 180 {
			panic("invalid receiver coordinate")
		}
		h, e := terrain.Height(receivers[i].lat, receivers[i].lon)
		must(e)
		n, e := geoid.Height(receivers[i].lat, receivers[i].lon)
		must(e)
		receivers[i].height = h + n + 1.5
	}
	velocities := [][2]float64{{0, 0}}
	if *motion > 0 {
		for i := 0; i < 8; i++ {
			a := float64(i) * math.Pi / 4
			velocities = append(velocities, [2]float64{*motion * math.Cos(a), *motion * math.Sin(a)})
		}
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	must(os.MkdirAll(*out, 0700))
	exe, e := os.Executable()
	must(e)
	summary := []any{}
	for _, r := range cfg.Recordings {
		if r.ID == "" || filepath.Base(r.ID) != r.ID {
			panic("invalid recording id")
		}
		dir := filepath.Join(*out, r.ID)
		must(os.MkdirAll(dir, 0700))
		data, e := os.ReadFile(filepath.Join(r.Analysis, "detection.json"))
		must(e)
		var detection struct {
			Events []acoustic.Event `json:"events"`
		}
		must(json.Unmarshal(data, &detection))
		f, e := os.Open(r.Tracks)
		must(e)
		ts, e := tracks.Read(f)
		f.Close()
		must(e)
		var smoothing []tracks.SmoothingReport
		if *smoothStep > 0 {
			for i := range ts {
				smoothed, report, e := ts[i].Smooth(tracks.SmoothingOptions{Step: *smoothStep, MaxGap: 30, MaxAcceleration: 20})
				must(e)
				ts[i] = smoothed
				smoothing = append(smoothing, report)
			}
		}
		var all []fitRow
		var schedules []schedule
		winners := map[string]fitRow{}
		for _, p := range receivers {
			for clock := -*clockWindow; clock <= *clockWindow+1e-9; clock += *clockStep {
				for _, v := range velocities {
					h := search.CurveReceiver{ReceiverHypothesis: search.ReceiverHypothesis{Lat: p.lat, Lon: p.lon, Height: p.height, RecordingStart: r.Start, Clock: clock, SoundSpeed: 343, MaxGap: 30}, East: v[0], North: v[1]}
					var choices []fitRow
					for index, event := range detection.Events {
						if event.Evidence == nil || event.Evidence.Level != "strong" && (!*includeReview || event.Evidence.Level != "review") {
							continue
						}
						var best fitRow
						found := false
						for _, t := range ts {
							if err := ctx.Err(); err != nil {
								panic(err)
							}
							start, e := time.Parse(time.RFC3339Nano, r.Start)
							must(e)
							epoch := float64(start.Unix()) + float64(start.Nanosecond())/1e9 + clock
							a, ok := t.ApproachWithin(geo.ECEF(p.lat, p.lon, p.height), epoch+event.Start-10, epoch+event.End+10, 343, 30)
							if !ok || a.Distance > 15000+*motion*event.End {
								continue
							}
							window := event.Resolution
							if window == 0 {
								window = 2
							}
							review, e := search.FitRidgeCurve(ctx, h, t, event, search.CurveModelOptions{Window: window, MaxSourceChange: *drift})
							must(e)
							fit, ok := selected(review)
							if !ok {
								continue
							}
							row := fitRow{Event: index + 1, Level: event.Evidence.Level, Start: event.Start, End: event.End, Track: t.ID, Receiver: p.label, Path: h, Fit: fit, TrainingGain: utility(fit)}
							row.Fit.Curve = nil
							all = append(all, row)
							if !found || fit.Criterion < best.Fit.Criterion {
								best = row
								best.Fit = fit
								found = true
							}
						}
						if found {
							choices = append(choices, best)
							key := fmt.Sprintf("%s/%d", p.label, index+1)
							old, exists := winners[key]
							if !exists || best.Fit.Criterion < old.Fit.Criterion {
								winners[key] = best
							}
						}
					}
					// Freeze distinct, nonoverlapping curve assignments using only
					// training gains. Validation cannot replace a failed assignment.
					sort.SliceStable(choices, func(i, j int) bool { return choices[i].TrainingGain > choices[j].TrainingGain })
					sc := schedule{Receiver: p.label, Path: h, Events: []fitRow{}, Weakest: 0, ReservedPass: true}
					for _, choice := range choices {
						if choice.TrainingGain < .1 {
							continue
						}
						conflict := false
						for _, prev := range sc.Events {
							if aircraft(choice.Track) == aircraft(prev.Track) || choice.Start < prev.End+4 && choice.End > prev.Start-4 {
								conflict = true
								break
							}
						}
						if conflict {
							continue
						}
						choice.Fit.Curve = nil
						sc.Events = append(sc.Events, choice)
						sc.Weakest = choice.TrainingGain
						sc.ReservedPass = sc.ReservedPass && choice.Fit.Shape
						if len(sc.Events) == 2 {
							break
						}
					}
					if len(sc.Events) < 2 {
						sc.ReservedPass = false
					}
					schedules = append(schedules, sc)
				}
			}
		}
		for _, p := range receivers {
			best := -1
			for i, sc := range schedules {
				if sc.Receiver != p.label {
					continue
				}
				if best < 0 || len(sc.Events) > len(schedules[best].Events) || len(sc.Events) == len(schedules[best].Events) && sc.Weakest > schedules[best].Weakest {
					best = i
				}
			}
			if best >= 0 {
				schedules[best].Selected = true
			}
		}
		var kept []fitRow
		for _, w := range winners {
			kept = append(kept, w)
		}
		sort.Slice(kept, func(i, j int) bool {
			if kept[i].Event != kept[j].Event {
				return kept[i].Event < kept[j].Event
			}
			return kept[i].Receiver < kept[j].Receiver
		})
		save(filepath.Join(dir, "models.json"), map[string]any{"recording": r, "fits": all, "training_selected_review_winners": kept, "shared_clock_schedules": schedules, "max_source_change": *drift, "include_review": *includeReview, "smoothing": smoothing, "smooth_track_step_s": *smoothStep, "note": "Disclosed receiver alternatives; no position search. All motions are hypothetical straight paths. Per-event review winners can have different clocks/motions and MUST NOT be combined. Only shared_clock_schedules lock one path/clock and distinct nonoverlapping aircraft. Ridge discovery already uses full audio; reserved residuals are descriptive. Optional interpolation uses telemetry only, never audio or receiver. No aircraft identity, audibility, silence interval or GPS location is independently established."})
		body := strings.Builder{}
		for _, w := range kept {
			fmt.Fprintf(&body, "<h2>Event %d · %s · %s tier</h2><p>%s · clock %+.1f s · east/north %.2f / %.2f m/s · %s · source change %.1f%%</p><p>Training / reserved error %.2f / %.2f Hz; reserved straight-line error %.2f Hz; descriptive shape agreement %t.</p>", w.Event, html.EscapeString(w.Track), html.EscapeString(w.Level), w.Receiver, w.Path.Clock, w.Path.East, w.Path.North, w.Fit.Model, w.Fit.Change*100, w.Fit.Training, w.Fit.Reserved, w.Fit.Linear, w.Fit.Shape)
			observed := detection.Events[w.Event-1].Ridge
			lo, hi := math.Inf(1), 0.
			for _, q := range observed {
				lo = math.Min(lo, q.Hz)
				hi = math.Max(hi, q.Hz)
			}
			lo -= math.Max(2, (hi-lo)*.1)
			hi += math.Max(2, (hi-lo)*.1)
			path := func(points []acoustic.RidgeSample) string {
				var s strings.Builder
				for i, q := range points {
					op := "L"
					if i == 0 {
						op = "M"
					}
					fmt.Fprintf(&s, "%s%.2f,%.2f ", op, 900*(q.Time-w.Start)/(w.End-w.Start), 250*(hi-q.Hz)/(hi-lo))
				}
				return s.String()
			}
			fmt.Fprintf(&body, "<svg viewBox='0 0 900 250' role='img' aria-label='Measured ridge and predicted curve'><path d='%s' stroke='#84d8cf' fill='none'/><path d='%s' stroke='#ffb969' fill='none'/></svg><p>Time %.2f–%.2f s, frequency %.1f–%.1f Hz. Teal: extracted measured ridge. Orange: flight/source/motion prediction. This is a ridge plot, not a spectrogram.</p>", path(observed), path(w.Fit.Curve), w.Start, w.End, lo, hi)
		}
		must(os.WriteFile(filepath.Join(dir, "index.html"), []byte("<!doctype html><meta charset='utf-8'><title>Source and receiver model study</title><style>body{font:16px system-ui;max-width:1250px;margin:30px auto;background:#15252b;color:#e2eeee;padding:20px}a{color:#8fdfd0}svg{width:100%;background:#0c1720}p{line-height:1.5}</style><h1>Source and receiver model study</h1><p>Experimental fits at supplied receiver alternatives. Per-event winners cannot establish a shared flight sequence or a unique location. Source variation and motion may also improve incorrect-location fits.</p><a href='models.json'>All fits and shared-clock schedules</a>"+body.String()), 0600))
		good := 0
		for _, w := range kept {
			if w.Fit.Shape {
				good++
			}
		}
		summary = append(summary, map[string]any{"recording": r.ID, "geometry_and_model_trials": len(all), "review_winners": len(kept), "descriptive_shape_agreements": good, "note": "Counts can include multiple receiver hypotheses for the same sound; not a count of aircraft."})
		fmt.Printf("%s: %d model trials, %d review winners, %d descriptive shape agreements\n", r.ID, len(all), len(kept), good)
	}
	save(filepath.Join(*out, "summary.json"), map[string]any{"recordings": summary, "config": *config, "config_sha256": sha(*config), "executable_sha256": sha(exe), "max_source_change": *drift, "motion_speed": *motion, "clock_window": *clockWindow, "clock_step": *clockStep, "created_utc": time.Now().UTC(), "note": "Exploratory model diagnostics, not blind location or calibrated accuracy. No detection labels are promoted."})
}
