package search

import (
	"encoding/json"
	"fmt"
	"html/template"
	"math"
	"os"
	"path/filepath"
	"sort"

	"pinpoint/internal/acoustic"
)

// WriteGuidedReport keeps the seeded map/results intact. Displayed lines are
// flight predictions; only the spectrogram is measured acoustic evidence.
func WriteGuidedReport(dir string, r GuidedReport, s *acoustic.Spectrum) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	if err := JSON(filepath.Join(dir, "guided.json"), r); err != nil {
		return err
	}
	type figure struct {
		Candidate int
		Match     GuidedMatch
		Image     acoustic.SpectralImage
		Path      string
	}
	var figures []figure
	features := []any{}
	for i, c := range r.Candidates {
		features = append(features, map[string]any{"type": "Feature", "geometry": map[string]any{"type": "Point", "coordinates": []float64{c.Seed.Lon, c.Seed.Lat}}, "properties": map[string]any{"rank_for_review": i + 1, "additional_flights_with_support": c.Supported, "clock_offset_s": c.Seed.ClockOffset, "anchor_rechecked": c.AnchorValid, "confirmed_location": false}})
		if i >= 5 {
			continue
		}
		// Show passing evidence before high training fits rejected on test audio.
		matches := append([]GuidedMatch(nil), c.Matches...)
		sort.SliceStable(matches, func(i, j int) bool { return matches[i].Supported && !matches[j].Supported })
		for j, m := range matches {
			if j >= 3 {
				break
			}
			lo, hi := math.Inf(1), 0.
			for _, p := range m.Curve {
				lo = math.Min(lo, p.Hz)
				hi = math.Max(hi, p.Hz)
			}
			if len(m.Curve) == 0 {
				continue
			}
			e := acoustic.Event{Start: m.Start, End: m.End, FMin: lo * .9, FMax: hi * 1.1, Harmonics: []int{1}}
			img, err := s.EventImage(e)
			if err != nil {
				return err
			}
			path := ""
			for k, p := range m.Curve {
				op := "L"
				if k == 0 || p.Time-m.Curve[k-1].Time > 2*s.Step()+1 {
					op = "M"
				}
				x := 900 * (p.Time - img.Start) / (img.End - img.Start)
				y := 280 * (img.MaxHz - p.Hz) / (img.MaxHz - img.MinHz)
				path += fmt.Sprintf("%s%.2f,%.2f ", op, x, y)
			}
			figures = append(figures, figure{i + 1, m, img, path})
		}
	}
	if err := JSON(filepath.Join(dir, "guided-candidates.geojson"), map[string]any{"type": "FeatureCollection", "features": features}); err != nil {
		return err
	}
	t, err := template.New("guided").Funcs(template.FuncMap{"json": func(v any) string { b, _ := json.Marshal(v); return string(b) }, "image": func(s string) template.URL { return template.URL(s) }, "margin": func(p *float64) string {
		if p == nil {
			return "unavailable"
		}
		return fmt.Sprintf("%.2f dB", *p)
	}}).Parse(guidedHTML)
	if err != nil {
		return err
	}
	f, err := os.Create(filepath.Join(dir, "guided.html"))
	if err != nil {
		return err
	}
	err = t.Execute(f, struct {
		Report  GuidedReport
		Figures []figure
	}{r, figures})
	closeErr := f.Close()
	if err != nil {
		return err
	}
	return closeErr
}

const guidedHTML = `<!doctype html><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>Additional flyover evidence</title>
<style>body{font:16px system-ui;background:#14252a;color:#e2eeee;max-width:1300px;margin:auto;padding:24px;line-height:1.5}a{color:#83d6c6}table{border-collapse:collapse;width:100%;margin-bottom:25px}td,th{padding:8px;text-align:left;border-bottom:1px solid #486069}svg{width:100%;background:#071116}.prediction{stroke:#ffad60;fill:none;stroke-width:2}body.hide .prediction{display:none}.note{color:#b9cbd0}summary{cursor:pointer}pre{white-space:pre-wrap}</style>
<h1>Additional flyover evidence</h1><p>{{.Report.Source}}</p><p>{{.Report.Note}}</p>
{{if .Report.SeedNote}}<p><strong>Seed search note:</strong> {{.Report.SeedNote}}</p>{{end}}
{{with .Report.Sequence}}<h2>Whole-sequence timing audit</h2><p><strong>{{.Conclusion}}</strong></p><p>{{.Note}}</p>
<p>Required additional aircraft: {{.Extra}}. Separated from wrong-time searches: {{.Separated}}. Competing sequences: {{.Competing}}. Margin over the best wrong-time search: {{margin .ControlMargin}}.</p>
<table><tr><th>Index (zero-based)</th><th>Position / clock</th><th>Paired controls complete</th><th>Additional encounters tested</th><th>Weakest heldout margin</th><th>Training-selected additional aircraft</th></tr>
{{range $i,$c:=.Candidates}}<tr><td>{{$i}}</td><td>{{printf "%.6f, %.6f / %+.2f s" .Seed.Lat .Seed.Lon .Seed.ClockOffset}}</td><td>{{.Paired}}</td><td>{{.Actual.Attempted}}</td><td>{{if .Actual.Valid}}{{printf "%.2f dB" .Actual.Test}}{{else}}insufficient encounters{{end}}</td><td>{{range .Actual.Events}}{{.Track}} at {{printf "%.1f s" .CPA}}; {{end}}</td></tr>{{end}}</table>
<details><summary>Best result across all candidates for each incorrect schedule</summary><table><tr><th>Shift</th><th>Best heldout margin</th><th>Candidate index</th></tr>{{range .Maxima}}<tr><td>{{printf "%+.0f s" .Shift}}</td><td>{{printf "%.2f dB" .Score}}</td><td>{{.Candidate}}</td></tr>{{end}}</table></details>{{end}}
<p>Original locations and clocks remain fixed. The table is sorted for review by additional supporting flights; it is not a calibrated location ranking. No detection-only count or lack of support is treated as silence.</p>
<table><tr><th>Position</th><th>Fixed clock</th><th>Anchor rechecked</th><th>Additional flights passing checks</th><th>Encounters tested</th></tr>{{range .Report.Candidates}}<tr><td>{{printf "%.6f, %.6f" .Seed.Lat .Seed.Lon}}</td><td>{{printf "%+.2f s" .Seed.ClockOffset}}</td><td>{{.AnchorValid}}</td><td>{{.Supported}}</td><td>{{len .Matches}}</td></tr>{{end}}</table>
<p class="note">A large number of flight, frequency and location trials can produce chance matches even after these controls. Passing is a lead for review, not a confirmed aircraft. The same physical aircraft is counted once when its imported ICAO segment identifier is available.</p>
<p><a href="guided.json">All fits, thresholds and controls (JSON)</a> · <a href="guided-candidates.geojson">Candidate points (GeoJSON)</a></p>
<label><input type="checkbox" checked onchange="document.body.classList.toggle('hide',!this.checked)"> Show flight predictions over measured spectral power</label>
{{range .Figures}}<h2>Candidate {{.Candidate}} · {{.Match.Track}}</h2><p>Additional support: <strong>{{.Match.Supported}}</strong> · {{.Match.Type}} · {{printf "%.1f–%.1f s" .Match.Start .Match.End}} · predicted arrival CPA {{printf "%.2f s" .Match.CPA}} · gap from first anchor CPA {{printf "%.2f s" .Match.AnchorGap}}</p>
<p>Validation: {{.Match.Validation}} · controls: {{.Match.ControlMode}} · partial recording window: {{.Match.Partial}}. Pitch/window selection score {{printf "%.2f dB" .Match.Fit.Selection}} uses training evidence only.</p>
<p>Training / heldout contrast: {{printf "%.2f / %.2f dB" .Match.Fit.Train .Match.Fit.Heldout}} · heldout positive fraction {{printf "%.2f" .Match.Fit.Positive}} · margin over strongest control {{printf "%.2f dB" .Match.Margin}}. Rejection reasons: {{range .Match.Reasons}}{{.}}; {{else}}none under these experimental thresholds{{end}}.</p>
<svg viewBox="0 0 900 280" role="img" aria-label="Measured whitened spectrogram and predicted flight curve"><image href="{{image .Image.DataURL}}" width="900" height="280" preserveAspectRatio="none"/><path class="prediction" d="{{.Path}}"/></svg><p class="note">Time {{printf "%.1f–%.1f s" .Image.Start .Image.End}}; frequency {{printf "%.1f–%.1f Hz" .Image.MinHz .Image.MaxHz}}. Orange is a prediction, not an independently traced ridge. Remove it to inspect the measured sound.</p>
<details><summary>Control fits</summary><p>Shifted-audio comparisons:</p><pre>{{json .Match.Controls}}</pre><p>Same-audio competing trajectory shapes (correlated checks):</p><pre>{{json .Match.Alternatives}}</pre><p>Steady-tone control: {{printf "%.2f dB" .Match.Flat.Heldout}} heldout.</p>{{with .Match.Linear}}<p>Linear-tone control: {{printf "%.2f dB" .Heldout}} heldout; {{printf "%.2f Hz/s" .Slope}}.</p>{{end}}</details>{{end}}
`
