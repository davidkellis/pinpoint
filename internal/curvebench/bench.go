package curvebench

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"sync"
	"time"

	"pinpoint/internal/acoustic"
)

const Version = "curvebench-v1"
const MatchVersion = "anchors-v2"

var Methods = []string{"stft-1s", "stft-2s", "stft-4s", "stft-8s", "local-2s", "reassigned-1s", "reassigned-2s", "harmonic-2s", "production-2+4s"}

type Config struct {
	Cases   []Case   `json:"cases"`
	Methods []string `json:"methods,omitempty"`
}
type Match struct {
	HitFraction float64 `json:"anchor_hit_fraction"`
	Coverage    float64 `json:"anchor_coverage"`
	RMSE        float64 `json:"rmse_hz"`
	Level       string  `json:"evidence_level"`
	Start       float64 `json:"start_s"`
	End         float64 `json:"end_s"`
	Ratio       float64 `json:"harmonic_ratio"`
	Recovered   bool    `json:"recovered"`
	Strong      bool    `json:"strong"`
}
type Result struct {
	Method       string           `json:"method"`
	Milliseconds int64            `json:"milliseconds"`
	Fragments    int              `json:"fragments"`
	Strong       int              `json:"strong"`
	Review       int              `json:"review"`
	Match        *Match           `json:"known_curve_match,omitempty"`
	Events       []acoustic.Event `json:"events"`
	Plot         Grid             `json:"plot"`
}
type CaseReport struct {
	Version      string   `json:"version"`
	MatchVersion string   `json:"match_version"`
	Input        Case     `json:"input"`
	SHA256       string   `json:"audio_sha256"`
	Raw          Grid     `json:"raw"`
	Results      []Result `json:"results"`
}
type Summary struct {
	Version string       `json:"version"`
	Methods []string     `json:"methods"`
	Reports []CaseReport `json:"reports"`
	Errors  []string     `json:"errors,omitempty"`
}

func Validate(c Config) error {
	if len(c.Cases) == 0 {
		return fmt.Errorf("no cases")
	}
	seen := map[string]bool{}
	for _, method := range c.Methods {
		valid := false
		for _, known := range Methods {
			valid = valid || method == known
		}
		if !valid || seen[method] {
			return fmt.Errorf("unknown or duplicate method %q", method)
		}
		seen[method] = true
	}
	seen = map[string]bool{}
	safe := regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,70}$`)
	for _, x := range c.Cases {
		if !safe.MatchString(x.ID) || seen[x.ID] {
			return fmt.Errorf("invalid or duplicate case id %q", x.ID)
		}
		seen[x.ID] = true
		if x.Start < 0 || math.IsNaN(x.Start) || math.IsInf(x.Start, 0) || x.Duration < 35 || x.Duration > 7200 || math.IsNaN(x.Duration) || math.IsInf(x.Duration, 0) {
			return fmt.Errorf("%s: invalid start or duration (35–7200 s)", x.ID)
		}
		if x.Kind != "reference" && x.Kind != "unlabeled" && x.Kind != "injection" && x.Kind != "non_doppler" && x.Kind != "other_doppler" {
			return fmt.Errorf("%s: invalid kind", x.ID)
		}
		if x.Inject != nil && (x.Inject.Kind != "jet" && x.Inject.Kind != "prop" && x.Inject.Kind != "vehicle") {
			return fmt.Errorf("%s: invalid injection", x.ID)
		}
		if x.Inject != nil && (math.IsNaN(x.Inject.SNRDB) || math.IsInf(x.Inject.SNRDB, 0) || x.Inject.SNRDB < -100 || x.Inject.SNRDB > 40) {
			return fmt.Errorf("%s: injection SNR must be finite and between -100 and 40 dB", x.ID)
		}
		if x.Control != "" && x.Control != "chirp" && x.Control != "bursts" && x.Control != "shuffle" {
			return fmt.Errorf("%s: invalid control", x.ID)
		}
		for i, p := range x.Anchors {
			if p.Time < 0 || p.Time > x.Duration || p.Hz <= 0 || math.IsNaN(p.Hz) || math.IsNaN(p.Time) || math.IsInf(p.Hz, 0) || math.IsInf(p.Time, 0) || i > 0 && p.Time <= x.Anchors[i-1].Time {
				return fmt.Errorf("%s: invalid truth anchors", x.ID)
			}
		}
	}
	return nil
}
func JSON(name string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(name, b, 0600)
}
func Run(ctx context.Context, config Config, out string, workers int, progress func(string)) (Summary, error) {
	if len(config.Methods) == 0 {
		config.Methods = append([]string(nil), Methods...)
	}
	summary := Summary{Version: Version, Methods: config.Methods, Reports: make([]CaseReport, len(config.Cases))}
	if err := Validate(config); err != nil {
		return summary, err
	}
	if workers < 1 || workers > 4 {
		return summary, fmt.Errorf("workers must be 1–4")
	}
	if err := os.MkdirAll(out, 0755); err != nil {
		return summary, err
	}
	if err := JSON(filepath.Join(out, "manifest.json"), config); err != nil {
		return summary, err
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	next := 0
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				mu.Lock()
				i := next
				next++
				mu.Unlock()
				if i >= len(config.Cases) {
					return
				}
				c := config.Cases[i]
				report, err := runCase(ctx, c, config.Methods, out, func(m string) {
					mu.Lock()
					defer mu.Unlock()
					if progress != nil {
						progress(m)
					}
				})
				mu.Lock()
				summary.Reports[i] = report
				if err != nil {
					summary.Errors = append(summary.Errors, fmt.Sprintf("%s: %v", c.ID, err))
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if err := JSON(filepath.Join(out, "summary.json"), compact(summary)); err != nil {
		return summary, err
	}
	if err := writeIndex(out, summary); err != nil {
		return summary, err
	}
	if len(summary.Errors) > 0 {
		return summary, fmt.Errorf("%d cases failed; see summary.json", len(summary.Errors))
	}
	return summary, nil
}
func runCase(ctx context.Context, c Case, methods []string, out string, progress func(string)) (CaseReport, error) {
	report := CaseReport{Version: Version, MatchVersion: MatchVersion, Input: c}
	dir := filepath.Join(out, c.ID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return report, err
	}
	audio, hash, err := decode(ctx, c)
	if err != nil {
		return report, err
	}
	report.SHA256 = hash
	if c.Inject != nil {
		audio, report.Input.Anchors = inject(audio, c.Inject.Kind, c.Inject.SNRDB)
	}
	base := map[int]*acoustic.Spectrum{}
	for _, seconds := range []int{2, 4} {
		raw, e := transform(ctx, audio, seconds, false)
		if e != nil {
			return report, e
		}
		if seconds == 2 {
			report.Raw = makeGrid(raw, true)
		}
		base[seconds] = whiten(raw)
	}
	reports := map[string]acoustic.DetectionReport{}
	// The combined baseline needs both standard passes even when they are not
	// separately selected for display. Never combine empty reports.
	standard := func(seconds int) acoustic.DetectionReport {
		name := fmt.Sprintf("stft-%ds", seconds)
		if d, ok := reports[name]; ok {
			return d
		}
		other := 4
		if seconds == 4 {
			other = 2
		}
		d := acoustic.CorroborateCurves(base[seconds].DiscoverAll(), base[other])
		reports[name] = d
		return d
	}
	for _, method := range methods {
		if err = ctx.Err(); err != nil {
			return report, err
		}
		start := time.Now()
		var s *acoustic.Spectrum
		var detection acoustic.DetectionReport
		switch method {
		case "stft-2s":
			s = base[2]
		case "stft-4s":
			s = base[4]
		case "stft-1s", "stft-8s", "reassigned-1s", "reassigned-2s":
			seconds := 1
			if method == "stft-8s" {
				seconds = 8
			}
			if method == "reassigned-2s" {
				seconds = 2
			}
			raw, e := transform(ctx, audio, seconds, method == "reassigned-1s" || method == "reassigned-2s")
			if e != nil {
				return report, e
			}
			s = whiten(raw)
		case "local-2s":
			s = localContrast(base[2])
		case "harmonic-2s":
			s = harmonicPool(base[2])
		case "production-2+4s":
			s = base[4]
			detection = acoustic.CombineDetections(s, standard(2), standard(4))
		}
		if method != "production-2+4s" {
			if method == "stft-2s" {
				detection = standard(2)
			} else if method == "stft-4s" {
				detection = standard(4)
			} else {
				other := base[4]
				if s.Window == other.Window {
					other = base[2]
				}
				detection = acoustic.CorroborateCurves(s.DiscoverAll(), other)
			}
			reports[method] = detection
		}
		r := Result{Method: method, Milliseconds: time.Since(start).Milliseconds(), Fragments: len(detection.Events), Strong: detection.Strong, Review: detection.Review, Match: match(detection.Events, report.Input.Anchors), Plot: makeGrid(s, false)}
		// Weak proposals remain in the detailed report, but do not clutter the viewer.
		for _, e := range detection.Events {
			if e.Evidence != nil && e.Evidence.Level != "weak" {
				r.Events = append(r.Events, e)
			}
		}
		if err = JSON(filepath.Join(dir, method+"-detections.json"), detection); err != nil {
			return report, err
		}
		report.Results = append(report.Results, r)
		progress(fmt.Sprintf("%s / %s: %d strong, %d review%s", c.ID, method, r.Strong, r.Review, matchMessage(r.Match)))
	}
	if err = JSON(filepath.Join(dir, "report.json"), report); err != nil {
		return report, err
	}
	if err = writeCase(dir, report); err != nil {
		return report, err
	}
	return report, nil
}
func matchMessage(m *Match) string {
	if m == nil {
		return " (no labeled path)"
	}
	return fmt.Sprintf("; known path %.0f%% (%s)", m.HitFraction*100, m.Level)
}
func match(events []acoustic.Event, truth []acoustic.RidgeSample) *Match {
	if len(truth) == 0 {
		return nil
	}
	best := &Match{Level: "not_found"}
	for _, e := range events {
		for _, ratio := range []float64{1, 2, 3, .5, 1. / 3} {
			count, hits := 0, 0
			ss := 0.
			for _, p := range truth {
				if hz, ok := e.RidgeHz(p.Time); ok {
					err := hz/ratio - p.Hz
					count++
					ss += err * err
					if math.Abs(err) <= math.Max(3, p.Hz*.015) {
						hits++
					}
				}
			}
			if count == 0 {
				continue
			}
			coverage := float64(count) / float64(len(truth))
			hit := float64(hits) / float64(len(truth))
			rmse := math.Sqrt(ss / float64(count))
			level := "unknown"
			if e.Evidence != nil {
				level = e.Evidence.Level
			}
			candidate := &Match{HitFraction: hit, Coverage: coverage, RMSE: rmse, Level: level, Start: e.Start, End: e.End, Ratio: ratio, Recovered: hit >= .8, Strong: hit >= .8 && level == "strong"}
			if candidate.Strong && !best.Strong || candidate.Strong == best.Strong && (hit > best.HitFraction || hit == best.HitFraction && hit > 0 && rmse < best.RMSE) {
				best = candidate
			}
		}
	}
	return best
}
func compact(s Summary) Summary {
	r := s
	r.Reports = append([]CaseReport(nil), s.Reports...)
	for i, c := range r.Reports {
		c.Raw = Grid{}
		c.Results = append([]Result(nil), c.Results...)
		for j := range c.Results {
			c.Results[j].Plot = Grid{}
			c.Results[j].Events = nil
		}
		r.Reports[i] = c
	}
	sort.Strings(r.Errors)
	return r
}
