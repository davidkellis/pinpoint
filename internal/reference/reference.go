// Package reference runs frozen, independently labeled development regressions.
// Evaluation labels never enter acoustic detection or geographic scoring.
package reference

import (
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/gob"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"pinpoint/internal/acoustic"
	"pinpoint/internal/geo"
	"pinpoint/internal/search"
	"pinpoint/internal/tracks"
)

type Asset struct {
	Path     string   `json:"path"`
	SHA256   string   `json:"sha256"`
	Profiles []string `json:"profiles"`
	Source   string   `json:"source"`
}
type Label struct {
	ID            string                 `json:"id"`
	Anchors       []acoustic.RidgeSample `json:"anchors"`
	MinHit        float64                `json:"min_hit_fraction"`
	MinCoverage   float64                `json:"min_coverage"`
	MaxRMSE       float64                `json:"max_rmse_hz"`
	RequireStrong bool                   `json:"require_strong"`
}
type AcousticCase struct {
	DetectorProfile string   `json:"detector_profile,omitempty"`
	MaxStrong       *int     `json:"max_strong_candidates,omitempty"`
	ID              string   `json:"id"`
	Profiles        []string `json:"profiles"`
	Audio           string   `json:"audio,omitempty"`
	Long            string   `json:"long_spectrum,omitempty"`
	Short           string   `json:"short_spectrum,omitempty"`
	Offset          float64  `json:"time_offset_s,omitempty"`
	Labels          []Label  `json:"labels"`
	Note            string   `json:"note"`
}
type LocationCase struct {
	DetectorProfile  string         `json:"detector_profile,omitempty"`
	EventSelection   string         `json:"event_selection,omitempty"`
	ID               string         `json:"id"`
	Profiles         []string       `json:"profiles"`
	Audio            string         `json:"audio"`
	Configs          []string       `json:"configs"`
	ExpectedAircraft []string       `json:"expected_aircraft"`
	ExpectedRegion   geo.Bounds     `json:"expected_region"`
	RequireAmbiguity bool           `json:"require_ambiguity"`
	KnownLocation    *LocationLabel `json:"known_location,omitempty"`
	Note             string         `json:"note"`
}

// LocationLabel is evaluation-only ground truth. It is never copied to Config,
// detection, candidate generation, or scoring. Support does not mean a unique fix.
type LocationLabel struct {
	Lat           float64 `json:"lat"`
	Lon           float64 `json:"lon"`
	SupportRadius float64 `json:"support_radius_m"`
	MaxScoreGap   float64 `json:"max_score_gap_db"`
}
type LocationEvaluation struct {
	BestError       float64  `json:"best_position_error_m"`
	SupportDistance *float64 `json:"nearest_supported_point_m,omitempty"`
	ScoreGap        *float64 `json:"supported_point_score_gap_db,omitempty"`
}
type Suite struct {
	Version   int            `json:"version"`
	Note      string         `json:"note"`
	Assets    []Asset        `json:"assets"`
	Acoustic  []AcousticCase `json:"acoustic"`
	Locations []LocationCase `json:"locations"`
}
type CurveResult struct {
	ID       string   `json:"id"`
	Hit      float64  `json:"anchor_hit_fraction"`
	Coverage float64  `json:"anchor_coverage"`
	RMSE     *float64 `json:"rmse_hz,omitempty"`
	Ratio    float64  `json:"harmonic_ratio"`
	Strong   bool     `json:"strong"`
	Pass     bool     `json:"pass"`
}
type CaseResult struct {
	ID            string              `json:"id"`
	Status        string              `json:"status"`
	Seconds       float64             `json:"seconds"`
	Fragments     int                 `json:"fragments,omitempty"`
	Strong        int                 `json:"strong_candidates,omitempty"`
	Curves        []CurveResult       `json:"curves,omitempty"`
	Best          *search.Candidate   `json:"best,omitempty"`
	Groups        []search.Candidate  `json:"flight_groups,omitempty"`
	Evaluated     int                 `json:"locations_evaluated,omitempty"`
	Ambiguity     string              `json:"ambiguity,omitempty"`
	KnownLocation *LocationEvaluation `json:"known_location_evaluation,omitempty"`
	Failures      []string            `json:"failures,omitempty"`
	Note          string              `json:"note"`
}
type Report struct {
	Version        int          `json:"version"`
	ManifestSHA256 string       `json:"manifest_sha256"`
	CodeSHA256     string       `json:"go_source_sha256"`
	GoVersion      string       `json:"go_version"`
	Started        string       `json:"started_utc"`
	Profile        string       `json:"profile"`
	Status         string       `json:"status"`
	VerifiedAssets int          `json:"verified_assets"`
	Cases          []CaseResult `json:"cases"`
	Errors         []string     `json:"errors,omitempty"`
}

func ReadJSON(name string, v any) error {
	f, err := os.Open(name)
	if err != nil {
		return err
	}
	defer f.Close()
	d := json.NewDecoder(f)
	d.DisallowUnknownFields()
	if err = d.Decode(v); err != nil {
		return err
	}
	var extra any
	if err = d.Decode(&extra); err != io.EOF {
		return fmt.Errorf("%s: trailing JSON", name)
	}
	return nil
}
func selected(profiles []string, profile string) bool {
	for _, p := range profiles {
		if p == profile || profile == "all" {
			return true
		}
	}
	return false
}
func safePath(p string) bool { return p != "" && filepath.IsLocal(p) }
func hashFile(p string) (string, error) {
	f, e := os.Open(p)
	if e != nil {
		return "", e
	}
	defer f.Close()
	h := sha256.New()
	if _, e = io.Copy(h, f); e != nil {
		return "", e
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func codeHash(root string) (string, error) {
	h := sha256.New()
	for _, dir := range []string{"cmd", "internal"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(p string, d os.DirEntry, e error) error {
			if e != nil {
				return e
			}
			if d.IsDir() || !strings.HasSuffix(p, ".go") {
				return nil
			}
			b, e := os.ReadFile(p)
			if e != nil {
				return e
			}
			rel, _ := filepath.Rel(root, p)
			fmt.Fprintf(h, "%s\x00%d\x00", rel, len(b))
			h.Write(b)
			return nil
		})
		if err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Verify fails closed: unavailable or altered inputs never count as a pass.
func Verify(root string, s Suite, profile string) (int, error) {
	if s.Version != 1 {
		return 0, fmt.Errorf("unsupported suite version %d", s.Version)
	}
	if profile != "quick" && profile != "audio" && profile != "location" && profile != "all" {
		return 0, fmt.Errorf("unknown suite %q", profile)
	}
	assets := map[string]bool{}
	n := 0
	for _, a := range s.Assets {
		if !safePath(a.Path) {
			return n, fmt.Errorf("invalid asset path %q", a.Path)
		}
		if !selected(a.Profiles, profile) {
			continue
		}
		if assets[a.Path] {
			return n, fmt.Errorf("duplicate asset %s", a.Path)
		}
		got, err := hashFile(filepath.Join(root, a.Path))
		if err != nil {
			return n, err
		}
		if got != a.SHA256 {
			return n, fmt.Errorf("checksum mismatch: %s", a.Path)
		}
		assets[a.Path] = true
		n++
	}
	need := func(p string) error {
		if !assets[p] {
			return fmt.Errorf("input has no verified checksum: %s", p)
		}
		return nil
	}
	seen := map[string]bool{}
	count, gates := 0, 0
	for _, c := range s.Acoustic {
		if !selected(c.Profiles, profile) {
			continue
		}
		count++
		if !safeID(c.ID) || seen[c.ID] {
			return n, fmt.Errorf("invalid/duplicate case id %q", c.ID)
		}
		seen[c.ID] = true
		if c.Audio != "" {
			if c.Long != "" || c.Short != "" {
				return n, fmt.Errorf("%s: choose audio or spectra", c.ID)
			}
			if err := need(c.Audio); err != nil {
				return n, err
			}
		} else {
			for _, p := range []string{c.Long, c.Short} {
				if err := need(p); err != nil {
					return n, err
				}
			}
		}
		for _, l := range c.Labels {
			gates++
			if len(l.Anchors) < 3 || l.MinHit <= 0 || l.MinHit > 1 || l.MinCoverage <= 0 || l.MinCoverage > 1 || l.MaxRMSE <= 0 {
				return n, fmt.Errorf("%s: invalid label", c.ID)
			}
			for i, p := range l.Anchors {
				if !finite(p.Time) || !finite(p.Hz) || p.Time < c.Offset || p.Hz <= 0 || i > 0 && p.Time <= l.Anchors[i-1].Time {
					return n, fmt.Errorf("%s: invalid anchors", c.ID)
				}
			}
		}
		if c.DetectorProfile != "" && c.DetectorProfile != "standard" && c.DetectorProfile != acoustic.ShortPassProfile && c.DetectorProfile != acoustic.HybridProfile {
			return n, fmt.Errorf("%s: invalid detector profile", c.ID)
		}
		if (c.DetectorProfile == acoustic.ShortPassProfile || c.DetectorProfile == acoustic.HybridProfile) && c.Audio == "" {
			return n, fmt.Errorf("%s: short-pass requires original audio", c.ID)
		}
		if c.MaxStrong != nil {
			gates++
			if *c.MaxStrong < 0 {
				return n, fmt.Errorf("%s: negative maximum strong count", c.ID)
			}
		}
	}
	for _, c := range s.Locations {
		if !selected(c.Profiles, profile) {
			continue
		}
		count++
		gates++
		if !safeID(c.ID) || seen[c.ID] || len(c.Configs) == 0 || len(c.ExpectedAircraft) == 0 {
			return n, fmt.Errorf("invalid location case %s", c.ID)
		}
		seen[c.ID] = true
		if c.DetectorProfile != "" && c.DetectorProfile != "standard" && c.DetectorProfile != acoustic.ShortPassProfile && c.DetectorProfile != acoustic.HybridProfile {
			return n, fmt.Errorf("%s: invalid location detector profile", c.ID)
		}
		if c.EventSelection != "" && c.EventSelection != "longest_usable" {
			return n, fmt.Errorf("%s: invalid location event selection", c.ID)
		}
		if c.EventSelection == "longest_usable" && (c.DetectorProfile == "" || len(c.ExpectedAircraft) != 1) {
			return n, fmt.Errorf("%s: longest_usable requires automatic detection and a single-passage reference", c.ID)
		}
		if p := c.KnownLocation; p != nil {
			if !finite(p.Lat) || !finite(p.Lon) || math.Abs(p.Lat) > 90 || math.Abs(p.Lon) > 180 || !finite(p.SupportRadius) || p.SupportRadius <= 0 || !finite(p.MaxScoreGap) || p.MaxScoreGap < 0 {
				return n, fmt.Errorf("%s: invalid known location label", c.ID)
			}
		}
		if err := need(c.Audio); err != nil {
			return n, err
		}
		for _, p := range c.Configs {
			if err := need(p); err != nil {
				return n, err
			}
			var cfg search.Config
			if err := ReadJSON(filepath.Join(root, p), &cfg); err != nil {
				return n, err
			}
			for _, input := range []string{cfg.Tracks, cfg.GeoidGrid} {
				if err := need(input); err != nil {
					return n, err
				}
			}
			if !safePath(cfg.TerrainDir) {
				return n, fmt.Errorf("invalid terrain path")
			}
			tiles, err := filepath.Glob(filepath.Join(root, cfg.TerrainDir, "*.hgt"))
			if err != nil || len(tiles) == 0 {
				return n, fmt.Errorf("missing terrain: %s", cfg.TerrainDir)
			}
			for _, tile := range tiles {
				rel, _ := filepath.Rel(root, tile)
				if err := need(rel); err != nil {
					return n, err
				}
			}
		}
	}
	if count == 0 || gates == 0 {
		return n, fmt.Errorf("suite has no asserted cases")
	}
	return n, nil
}
func safeID(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}
func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

// Match uses one continuous detected ridge and one harmonic ratio for all
// anchors. Isolated fragments or unrelated strong tones cannot be stitched
// together by the evaluator to pass a known flyover.
func Match(events []acoustic.Event, label Label, offset float64) CurveResult {
	best := CurveResult{ID: label.ID}
	for _, e := range events {
		for _, ratio := range []float64{1, 2, 3, .5, 1.0 / 3} {
			count, hits, ss := 0, 0, 0.0
			for _, p := range label.Anchors {
				hz, ok := e.RidgeHz(p.Time - offset)
				if !ok {
					continue
				}
				delta := hz/ratio - p.Hz
				count++
				ss += delta * delta
				if math.Abs(delta) <= math.Max(3, p.Hz*.015) {
					hits++
				}
			}
			if count == 0 {
				continue
			}
			rmse := math.Sqrt(ss / float64(count))
			r := CurveResult{ID: label.ID, Hit: float64(hits) / float64(len(label.Anchors)), Coverage: float64(count) / float64(len(label.Anchors)), RMSE: &rmse, Ratio: ratio, Strong: e.Evidence != nil && e.Evidence.Level == "strong"}
			r.Pass = r.Hit >= label.MinHit && r.Coverage >= label.MinCoverage && rmse <= label.MaxRMSE && (!label.RequireStrong || r.Strong)
			if r.Pass && !best.Pass || r.Pass == best.Pass && (r.Hit > best.Hit || r.Hit == best.Hit && (best.RMSE == nil || rmse < *best.RMSE)) {
				best = r
			}
		}
	}
	return best
}
func loadFixture(p string) (*acoustic.Spectrum, error) {
	f, e := os.Open(p)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	g, e := gzip.NewReader(f)
	if e != nil {
		return nil, e
	}
	defer g.Close()
	var s acoustic.Spectrum
	e = gob.NewDecoder(g).Decode(&s)
	return &s, e
}
func analyze(ctx context.Context, p string, seconds int) (*acoustic.Spectrum, error) {
	o := acoustic.Defaults()
	o.Window = 4096 * seconds
	o.Hop = o.Window / 8
	return acoustic.Analyze(ctx, p, o)
}
func runAcoustic(ctx context.Context, root string, c AcousticCase) (CaseResult, error) {
	r := CaseResult{ID: c.ID, Note: c.Note, Status: "pass"}
	if c.DetectorProfile == acoustic.HybridProfile {
		_, d, err := acoustic.AnalyzeHybrid(ctx, filepath.Join(root, c.Audio))
		if err != nil {
			return r, err
		}
		return evaluateAcoustic(c, r, d), nil
	}
	if c.DetectorProfile == acoustic.ShortPassProfile {
		_, d, err := acoustic.AnalyzeShortPass(ctx, filepath.Join(root, c.Audio), acoustic.ShortPassOptions())
		if err != nil {
			return r, err
		}
		return evaluateAcoustic(c, r, d), nil
	}
	var long, short *acoustic.Spectrum
	var err error
	if c.Audio != "" {
		long, err = analyze(ctx, filepath.Join(root, c.Audio), 4)
	} else {
		long, err = loadFixture(filepath.Join(root, c.Long))
	}
	if err != nil {
		return r, err
	}
	if c.Audio != "" {
		short, err = analyze(ctx, filepath.Join(root, c.Audio), 2)
	} else {
		short, err = loadFixture(filepath.Join(root, c.Short))
	}
	if err != nil {
		return r, err
	}
	if err = ctx.Err(); err != nil {
		return r, err
	}
	d := acoustic.CombineDetections(long, acoustic.CorroborateCurves(long.DiscoverAll(), short), acoustic.CorroborateCurves(short.DiscoverAll(), long))
	return evaluateAcoustic(c, r, d), nil
}

func evaluateAcoustic(c AcousticCase, r CaseResult, d acoustic.DetectionReport) CaseResult {
	r.Fragments = len(d.Events)
	r.Strong = d.Strong
	for _, l := range c.Labels {
		m := Match(d.Events, l, c.Offset)
		r.Curves = append(r.Curves, m)
		if !m.Pass {
			r.Failures = append(r.Failures, "did not recover "+l.ID)
		}
	}
	if c.MaxStrong != nil && d.Strong > *c.MaxStrong {
		r.Failures = append(r.Failures, fmt.Sprintf("%d strong candidates exceeds maximum %d", d.Strong, *c.MaxStrong))
	}
	if len(c.Labels) == 0 && c.MaxStrong == nil {
		r.Status = "unlabeled_observation"
	}
	if len(r.Failures) > 0 {
		r.Status = "fail"
	}
	return r
}

func EvaluateLocation(c LocationCase, groups []search.Candidate, surfaces ...[]search.ScorePoint) CaseResult {
	r := CaseResult{ID: c.ID, Status: "pass", Note: c.Note, Groups: groups}
	if len(groups) == 0 {
		r.Status = "fail"
		r.Failures = []string{"no candidates"}
		return r
	}
	r.Best = &groups[0]
	p := groups[0]
	b := c.ExpectedRegion
	if p.Lat < b.South || p.Lat > b.North || p.Lon < b.West || p.Lon > b.East {
		r.Failures = append(r.Failures, "top candidate outside reported regional label")
	}
	if len(p.Events) != len(c.ExpectedAircraft) {
		r.Failures = append(r.Failures, "wrong event count")
	} else {
		for i, id := range c.ExpectedAircraft {
			if strings.Split(p.Events[i].Track, "-")[0] != id {
				r.Failures = append(r.Failures, "wrong aircraft for event "+fmt.Sprint(i+1))
			}
		}
	}
	report := search.Report{FlightGroups: groups}
	for _, surface := range surfaces {
		report.ScoreGrid = append(report.ScoreGrid, surface...)
	}
	search.AssessAmbiguity(&report)
	r.Ambiguity = report.Ambiguity
	if c.RequireAmbiguity && r.Ambiguity == "" {
		r.Failures = append(r.Failures, "expected distant near-tie was lost; review search completeness and scoring")
	}
	if label := c.KnownLocation; label != nil {
		truth := geo.ECEF(label.Lat, label.Lon, 0)
		v := &LocationEvaluation{BestError: truth.Sub(geo.ECEF(p.Lat, p.Lon, 0)).Norm()}
		for _, q := range report.ScoreGrid {
			if !q.MeetsThreshold || q.Score == nil || p.Score-*q.Score > label.MaxScoreGap || len(q.TrackIDs) != len(c.ExpectedAircraft) {
				continue
			}
			identities := true
			for i, id := range c.ExpectedAircraft {
				if strings.Split(q.TrackIDs[i], "-")[0] != id {
					identities = false
					break
				}
			}
			if !identities {
				continue
			}
			distance := truth.Sub(geo.ECEF(q.Lat, q.Lon, 0)).Norm()
			if v.SupportDistance == nil || distance < *v.SupportDistance {
				gap := p.Score - *q.Score
				v.SupportDistance, v.ScoreGap = &distance, &gap
			}
		}
		r.KnownLocation = v
		if v.SupportDistance == nil || *v.SupportDistance > label.SupportRadius {
			r.Failures = append(r.Failures, "known recording location lost support among near-tied fits")
		}
	}
	if len(r.Failures) > 0 {
		r.Status = "fail"
	}
	return r
}

// locationEvents selects evidence before loading geographic or aircraft labels.
// A known single-passage benchmark can explicitly use the longest usable ridge;
// this does not change the web app's multi-event policy or turn engine tones into
// independent flights. No usable ridge remains a failed localization attempt.
func locationEvents(events []acoustic.Event, policy string) []acoustic.Event {
	usable := acoustic.SearchEvents(events, true, false)
	if policy != "longest_usable" || len(usable) == 0 {
		return usable
	}
	best := 0
	for i, event := range usable {
		duration := event.End - event.Start
		previous := usable[best].End - usable[best].Start
		if duration > previous || duration == previous && event.Start < usable[best].Start {
			best = i
		}
	}
	return usable[best : best+1]
}

func runLocation(ctx context.Context, root string, c LocationCase) (CaseResult, error) {
	var s *acoustic.Spectrum
	var events []acoustic.Event
	var err error
	if c.DetectorProfile == acoustic.ShortPassProfile || c.DetectorProfile == acoustic.HybridProfile {
		var detection acoustic.DetectionReport
		if c.DetectorProfile == acoustic.HybridProfile {
			s, detection, err = acoustic.AnalyzeHybrid(ctx, filepath.Join(root, c.Audio))
		} else {
			s, detection, err = acoustic.AnalyzeShortPass(ctx, filepath.Join(root, c.Audio), acoustic.ShortPassOptions())
		}
		events = locationEvents(detection.Events, c.EventSelection)
		if err == nil && len(events) != len(c.ExpectedAircraft) {
			err = fmt.Errorf("expected %d automatically usable events, found %d", len(c.ExpectedAircraft), len(events))
		}
	} else {
		if c.DetectorProfile == "standard" {
			var detection acoustic.DetectionReport
			s, detection, err = acoustic.AnalyzeMultiResolution(ctx, filepath.Join(root, c.Audio))
			events = locationEvents(detection.Events, c.EventSelection)
			if err == nil && len(events) != len(c.ExpectedAircraft) {
				err = fmt.Errorf("expected %d automatically usable events, found %d", len(c.ExpectedAircraft), len(events))
			}
		} else {
			s, err = analyze(ctx, filepath.Join(root, c.Audio), 4)
		}
	}
	if err != nil {
		return CaseResult{ID: c.ID}, err
	}
	groups := []search.Candidate{}
	var surface []search.ScorePoint
	evaluated := 0
	for _, name := range c.Configs {
		var cfg search.Config
		if err = ReadJSON(filepath.Join(root, name), &cfg); err != nil {
			return CaseResult{ID: c.ID}, err
		}
		if c.DetectorProfile != "" {
			cfg.Events = events // rerun detection; never use cached event JSON
		}
		cfg.TerrainDir = filepath.Join(root, cfg.TerrainDir)
		cfg.GeoidGrid = filepath.Join(root, cfg.GeoidGrid)
		f, e := os.Open(filepath.Join(root, cfg.Tracks))
		if e != nil {
			return CaseResult{ID: c.ID}, e
		}
		ts, e := tracks.Read(f)
		f.Close()
		if e != nil {
			return CaseResult{ID: c.ID}, e
		}
		r, e := search.Run(ctx, cfg, s, ts)
		if e != nil {
			return CaseResult{ID: c.ID}, e
		}
		evaluated += r.Evaluated
		groups = append(groups, r.FlightGroups...)
		surface = append(surface, r.ScoreGrid...)
	}
	sort.Slice(groups, func(i, j int) bool {
		a, b := groups[i], groups[j]
		if a.Score != b.Score {
			return a.Score > b.Score
		}
		if a.Lat != b.Lat {
			return a.Lat < b.Lat
		}
		return a.Lon < b.Lon
	})
	dedup := []search.Candidate{}
	seen := map[string]bool{}
	for _, g := range groups {
		ids := []string{}
		for _, e := range g.Events {
			ids = append(ids, e.Track)
		}
		key := strings.Join(ids, "/")
		if !seen[key] {
			seen[key] = true
			dedup = append(dedup, g)
		}
	}
	r := EvaluateLocation(c, dedup, surface)
	r.Evaluated = evaluated
	return r, nil
}

// Run always reruns current code. It never reads prior detection or result JSON
// as a substitute for analysis, and it never rewrites the suite requirements.
func Run(ctx context.Context, root, manifest, profile string, progress func(string)) (Report, error) {
	r := Report{Version: 1, Profile: profile, Started: time.Now().UTC().Format(time.RFC3339), GoVersion: runtime.Version(), Status: "error"}
	var s Suite
	if err := ReadJSON(filepath.Join(root, manifest), &s); err != nil {
		return r, err
	}
	r.ManifestSHA256, _ = hashFile(filepath.Join(root, manifest))
	var err error
	r.CodeSHA256, err = codeHash(root)
	if err != nil {
		return r, err
	}
	r.VerifiedAssets, err = Verify(root, s, profile)
	if err != nil {
		r.Errors = append(r.Errors, err.Error())
		return r, err
	}
	r.Status = "pass"
	add := func(start time.Time, result CaseResult, e error) {
		result.Seconds = time.Since(start).Seconds()
		if e != nil {
			result.Status = "error"
			result.Failures = append(result.Failures, e.Error())
		}
		if result.Status == "fail" || result.Status == "error" {
			r.Status = "fail"
		}
		r.Cases = append(r.Cases, result)
		if progress != nil {
			progress(fmt.Sprintf("%s: %s (%.1fs)", result.ID, result.Status, result.Seconds))
		}
	}
	for _, c := range s.Acoustic {
		if !selected(c.Profiles, profile) {
			continue
		}
		if err = ctx.Err(); err != nil {
			return r, err
		}
		if progress != nil {
			progress("Analyzing " + c.ID)
		}
		start := time.Now()
		result, e := runAcoustic(ctx, root, c)
		add(start, result, e)
	}
	for _, c := range s.Locations {
		if !selected(c.Profiles, profile) {
			continue
		}
		if err = ctx.Err(); err != nil {
			return r, err
		}
		if progress != nil {
			progress("Searching " + c.ID)
		}
		start := time.Now()
		result, e := runLocation(ctx, root, c)
		add(start, result, e)
	}
	if r.Status != "pass" {
		return r, fmt.Errorf("reference checks failed; inspect report")
	}
	return r, nil
}
