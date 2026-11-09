package search

import (
	"fmt"
	"html/template"
	"os"
	"path/filepath"
)

// mapURL supplies either local artifact links or the web app's existing event
// file routes. Only actual-time groups link to maps; every control stays in JSON.
func WriteAutomaticReport(dir string, r AutomaticReport, mapURL func(int) string) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	if err := JSON(filepath.Join(dir, "automatic.json"), r); err != nil {
		return err
	}
	type row struct {
		GroupResult
		URL      string
		From, To string
	}
	var rows []row
	for _, g := range r.Results {
		if g.Trial != 0 {
			continue
		}
		x := row{GroupResult: g}
		if g.Best != nil {
			x.URL = mapURL(g.Pair)
		}
		x.From = fmt.Sprintf("%.1f–%.1f s", r.Curves[g.Events[0]].Start, r.Curves[g.Events[0]].End)
		x.To = fmt.Sprintf("%.1f–%.1f s", r.Curves[g.Events[1]].Start, r.Curves[g.Events[1]].End)
		rows = append(rows, x)
	}
	page, err := template.New("automatic").Funcs(template.FuncMap{"one": func(i int) int { return i + 1 }, "score": func(p *float64) string {
		if p == nil {
			return "No passing control fit"
		}
		return fmt.Sprintf("%.2f dB", *p)
	}}).Parse(`<!doctype html><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>Automatic flyover groups</title><style>body{background:#17282d;color:#e0eded;font:16px system-ui;padding:24px;max-width:1200px;margin:auto;line-height:1.5}a{color:#81d8c6}td,th{text-align:left;padding:8px;border-bottom:1px solid #476066}table{width:100%}.status{border-left:3px solid #e8ba70;padding:12px}small{color:#b2c4ca}</style><h1>Automatic flyover groups</h1><p class="status">Outcome: <strong>{{.Report.Status}}</strong>. These are candidate fits; a location is not independently verified.</p><p>{{.Report.Note}}</p><p>{{len .Report.Curves}} strong sound curves · {{.Report.Pairs}} eligible pairs · {{len .Report.Options.ControlShifts}} wrong-time searches.</p>{{if .Report.Best}}<p>Highest-scoring group: curves {{one (index .Report.Best.Events 0)}} and {{one (index .Report.Best.Events 1)}}. Position {{printf "%.6f, %.6f" .Report.Best.Best.Lat .Report.Best.Best.Lon}}, shared clock {{printf "%+.2f s" .Report.Best.Best.ClockOffset}}, score {{printf "%.2f dB" .Report.Best.Best.Score}}.</p>{{end}}<p>Best wrong-time fit: {{if .Report.Options.ControlShifts}}{{score .Report.ControlMax}}{{else}}Not run{{end}}{{if .Report.Margin}} · Separation: {{score .Report.Margin}}{{end}}.</p>{{if .Report.ExcludedCurves}}<p>{{len .Report.ExcludedCurves}} tonal proposals overlap reviewed silence and were excluded from flyover selection. The downloadable report lists them.</p>{{end}}<p><a href="automatic.json">Download all actual and control outcomes</a></p><table><thead><tr><th>First curve</th><th>Second curve</th><th>Result</th><th>Inspect</th></tr></thead><tbody>{{range .Rows}}<tr><td>{{.From}}</td><td>{{.To}}</td><td>{{.Status}}{{if .Best}} · {{printf "%.2f dB" .Best.Score}}{{end}}{{if .Error}}<br><small>{{.Error}}</small>{{end}}</td><td>{{if .URL}}<a href="{{.URL}}">Map and Doppler comparisons</a>{{end}}</td></tr>{{end}}</tbody></table>`)
	if err != nil {
		return err
	}
	f, err := os.Create(filepath.Join(dir, "automatic.html"))
	if err != nil {
		return err
	}
	err = page.Execute(f, struct {
		Report AutomaticReport
		Rows   []row
	}{r, rows})
	ce := f.Close()
	if err != nil {
		return err
	}
	return ce
}
