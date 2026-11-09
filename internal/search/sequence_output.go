package search

import (
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"strings"
)

func CurveGroupLabel(indices []int) string {
	a := make([]string, len(indices))
	for i, n := range indices {
		a[i] = fmt.Sprint(n + 1)
	}
	return strings.Join(a, " + ")
}

func WriteSequenceReport(dir string, r SequenceReport, mapURL func(int) string) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	if err := JSON(filepath.Join(dir, "automatic.json"), r); err != nil {
		return err
	}
	type row struct {
		SequenceGroup
		URL, Label string
	}
	type control struct {
		Shift                        float64
		Groups, Fits, Passed, Errors int
		Maximum                      *float64
		Reasons                      []Rejection
	}
	controls := make([]control, len(r.Options.ControlShifts))
	for i, shift := range r.Options.ControlShifts {
		controls[i].Shift = shift
	}
	var rows []row
	for _, g := range r.Groups {
		if g.Trial == 0 {
			x := row{SequenceGroup: g, Label: CurveGroupLabel(g.Indices)}
			if g.Best != nil {
				x.URL = mapURL(g.ID)
			}
			rows = append(rows, x)
		} else if g.Trial > 0 && g.Trial <= len(controls) {
			q := &controls[g.Trial-1]
			q.Groups++
			if g.Best != nil {
				q.Fits++
			}
			if g.Pass {
				q.Passed++
			}
			if g.Status == "error" {
				q.Errors++
				q.Reasons = append(q.Reasons, g.Reasons...)
			}
			if g.Validation != nil && (q.Maximum == nil || *g.Validation > *q.Maximum) {
				q.Maximum = g.Validation
			}
		}
	}
	var selected *SequenceGroup
	if r.Selected != nil {
		selected = &r.Groups[*r.Selected]
	}
	p, err := template.New("sequences").Funcs(template.FuncMap{"one": func(i int) int { return i + 1 }, "db": func(p *float64) string {
		if p == nil {
			return "Unavailable"
		}
		return fmt.Sprintf("%.2f dB", *p)
	}, "label": CurveGroupLabel}).Parse(sequencePage)
	if err != nil {
		return err
	}
	f, err := os.Create(filepath.Join(dir, "automatic.html"))
	if err != nil {
		return err
	}
	err = p.Execute(f, struct {
		Report   SequenceReport
		Selected *SequenceGroup
		Rows     []row
		Controls []control
	}{r, selected, rows, controls})
	closeErr := f.Close()
	if err != nil {
		return err
	}
	return closeErr
}

const sequencePage = `<!doctype html><html lang="en"><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>Flyover sequences and validation</title><style>body{background:#17282d;color:#e0eded;font:16px system-ui;line-height:1.5;padding:24px;max-width:1280px;margin:auto}a{color:#81d8c6}table{width:100%;border-collapse:collapse}td,th{text-align:left;vertical-align:top;padding:8px;border-bottom:1px solid #476066}.status{border-left:3px solid #e8ba70;padding:12px}small{color:#b2c4ca}.scroll{overflow:auto}details{margin:12px 0}code{overflow-wrap:anywhere}</style><h1>Flyover sequences and validation</h1><p class="status">Outcome: <strong>{{.Report.Status}}</strong>. Candidate aircraft and locations remain unverified.</p><p>Groups fit one location, one recording clock and distinct aircraft. Reserved intervals check frozen predictions. Steady tones, linear pitch changes and {{len .Report.Options.ControlShifts}} incorrect-time searches provide competing explanations.</p>{{if .Report.Error}}<p class="status">{{.Report.Error}}</p>{{end}}{{range .Report.Reasons}}<p class="status"><strong>{{.Code}}</strong>: {{.Detail}}</p>{{end}}{{if .Selected}}<p>Selected using training evidence: <strong>curves {{label .Selected.Indices}}</strong>, {{printf "%.6f, %.6f" .Selected.Best.Lat .Selected.Best.Lon}}, shared clock {{printf "%+.2f s" .Selected.Best.ClockOffset}}. Every selected event must pass validation; a failed event is not removed to rescue the group.</p><p>Weakest reserved-event margin: {{db .Selected.Validation}}. Maximum incorrect-time margin: {{if .Report.Options.ControlShifts}}{{db .Report.ControlMax}}{{else}}Not run{{end}}. {{if .Report.Margin}}Separation: {{db .Report.Margin}}.{{end}}</p>{{end}}<p><a href="automatic.json">Download all groups, controls, rejected-event reasons and provenance</a></p>{{if .Report.Revalidation}}<p class="status">Validation-only replay: training winners are unchanged. Map links show the original training artifacts; the reserved checks in this overview use the new validation implementation. Original report SHA-256: <code>{{.Report.Revalidation.ReportSHA}}</code>.</p>{{end}}<div class="scroll"><table><thead><tr><th>Curves</th><th>Training</th><th>Reserved audio</th><th>Inspect</th></tr></thead><tbody>{{range .Rows}}<tr><td>{{.Label}}</td><td>{{if .Best}}{{printf "%.2f dB" .Best.Score}} weakest event{{else}}No fit{{end}}</td><td>{{.Status}}{{if .Validation}} · {{db .Validation}}{{end}}{{range .Reasons}}<br><small>{{.Code}}: {{.Detail}}</small>{{end}}{{if .Checks}}<details><summary>Per-aircraft checks</summary><table><tr><th>Curve / flight</th><th>Samples</th><th>Signal</th><th>Outcome</th></tr>{{range .Checks}}<tr><td>{{one .EventIndex}} · <code>{{.Track}}</code><br>{{printf "%.2f Hz" .F0}} fitted on training</td><td>{{.TrainSamples}} training<br>{{.TestSamples}} reserved</td><td>Flight {{db .Test}}<br>Steady {{db .Flat}}<br>Linear {{db .Linear}}</td><td>{{if .Pass}}Pass{{else}}Rejected{{end}}{{range .Reasons}}<br><small>{{.Code}}: {{.Detail}}</small>{{end}}</td></tr>{{end}}</table></details>{{end}}</td><td>{{if .URL}}<a href="{{.URL}}">Map and Doppler comparisons</a>{{end}}</td></tr>{{end}}</tbody></table></div><h2>Incorrect-time controls</h2><p>Every control repeats the full group search. Training fits and reserved passes are separate counts; missing or failed work cannot establish a negative result.</p><div class="scroll"><table><tr><th>Time shift</th><th>Group outcomes</th><th>Training fits</th><th>Reserved passes</th><th>Maximum reserved margin</th><th>Errors</th></tr>{{range .Controls}}<tr><td>{{printf "%+.0f s" .Shift}}</td><td>{{.Groups}}{{if eq .Groups 0}} · Not searched{{end}}</td><td>{{.Fits}}</td><td>{{.Passed}}</td><td>{{db .Maximum}}</td><td>{{.Errors}}{{range .Reasons}}<br>{{.Code}}: {{.Detail}}{{end}}</td></tr>{{else}}<tr><td colspan="6">Not run</td></tr>{{end}}</table></div><details><summary>Validation scope and limitations</summary><p>{{.Report.Note}}</p><p>Final grid spacing is evaluated spacing, not demonstrated location accuracy. Broad jet noise, variable source pitch, wind and microphone motion can invalidate the model. Missing support does not prove aircraft absence.</p></details></html>`
