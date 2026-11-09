package search

import (
	"encoding/csv"
	"fmt"
	"html/template"
	"math"
	"os"
	"path/filepath"
	"strings"
)

// WriteDiagnostic writes a self-contained, offline report and numeric residuals.
func WriteDiagnostic(dir string, r DiagnosticReport) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	if err := JSON(filepath.Join(dir, "diagnostic.json"), r); err != nil {
		return err
	}
	f, err := os.Create(filepath.Join(dir, "residuals.csv"))
	if err != nil {
		return err
	}
	w := csv.NewWriter(f)
	w.Write([]string{"track_id", "event", "time_s", "observed_hz", "predicted_hz", "residual_hz", "implied_source_hz"})
	for _, fit := range r.Fits {
		for _, p := range fit.Points {
			w.Write([]string{fit.Track, fmt.Sprint(fit.Event), fmt.Sprint(p.Time), fmt.Sprint(p.Observed), fmt.Sprint(p.Predicted), fmt.Sprint(p.Residual), fmt.Sprint(p.ImpliedSource)})
		}
	}
	w.Flush()
	writeErr := w.Error()
	closeErr := f.Close()
	if writeErr != nil {
		return writeErr
	}
	if closeErr != nil {
		return closeErr
	}
	type panel struct {
		Fit                                                       FitDiagnostic
		Curve, Residual, Source                                   string
		Start, End, Low, High, MaxResidual, LowSource, HighSource float64
	}
	var panels []panel
	for _, fit := range r.Fits {
		p := panel{Fit: fit}
		if len(fit.Points) > 0 {
			p.Start, p.End = fit.Points[0].Time, fit.Points[len(fit.Points)-1].Time
			p.Low, p.LowSource = math.Inf(1), math.Inf(1)
			p.High, p.HighSource = math.Inf(-1), math.Inf(-1)
			for _, q := range fit.Points {
				p.Low = math.Min(p.Low, math.Min(q.Observed, q.Predicted))
				p.High = math.Max(p.High, math.Max(q.Observed, q.Predicted))
				p.MaxResidual = math.Max(p.MaxResidual, math.Abs(q.Residual))
				p.LowSource = math.Min(p.LowSource, q.ImpliedSource)
				p.HighSource = math.Max(p.HighSource, q.ImpliedSource)
			}
			path := func(value func(FitResidual) float64, lo, hi float64) string {
				var b strings.Builder
				for i, q := range fit.Points {
					command := "L"
					if i == 0 || q.Time-fit.Points[i-1].Time > 2 {
						command = "M"
					}
					fmt.Fprintf(&b, "%s%.2f,%.2f ", command, 20+960*(q.Time-p.Start)/math.Max(.001, p.End-p.Start), 180-160*(value(q)-lo)/math.Max(.001, hi-lo))
				}
				return b.String()
			}
			p.Curve = path(func(q FitResidual) float64 { return q.Observed }, p.Low, p.High) + "|" + path(func(q FitResidual) float64 { return q.Predicted }, p.Low, p.High)
			p.Residual = path(func(q FitResidual) float64 { return q.Residual }, -p.MaxResidual, p.MaxResidual)
			p.Source = path(func(q FitResidual) float64 { return q.ImpliedSource }, p.LowSource, p.HighSource)
		}
		panels = append(panels, p)
	}
	t := template.Must(template.New("diagnostic").Funcs(template.FuncMap{"paths": func(s string) []string { return strings.Split(s, "|") }, "number": func(v *float64) float64 {
		if v == nil {
			return 0
		}
		return *v
	}}).Parse(diagnosticHTML))
	f, err = os.Create(filepath.Join(dir, "diagnostic.html"))
	if err != nil {
		return err
	}
	err = t.Execute(f, struct {
		Report DiagnosticReport
		Panels []panel
	}{r, panels})
	closeErr = f.Close()
	if err != nil {
		return err
	}
	return closeErr
}

const diagnosticHTML = `<!doctype html><html lang="en"><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>Flight fit diagnostics</title>
<style>body{font:16px system-ui;background:#17252a;color:#dce7eb;max-width:1150px;margin:2rem auto;padding:0 1rem}section{border-top:1px solid #607078;padding:1rem 0}svg{width:100%;height:auto;background:#10181d}path{fill:none;stroke:#79d6c2;stroke-width:1.5}path:nth-of-type(2){stroke:#ffaf54}a{color:#79d6c2}dt{font-weight:bold}dd{margin-bottom:.5rem}</style>
<h1>Flight fit diagnostics</h1><p>{{.Report.Note}}</p><p>Hypothesis: {{printf "%.6f" .Report.Lat}}, {{printf "%.6f" .Report.Lon}}; clock correction {{printf "%.3f" .Report.Clock}} s; ellipsoid height {{printf "%.1f" .Report.Height}} m.</p>
<p><a href="diagnostic.json">All metrics (JSON)</a> · <a href="residuals.csv">Residuals (CSV)</a></p>
{{range .Panels}}<section><h2>Event {{.Fit.Event}} · {{.Fit.Track}}</h2><p>Scorer status: <b>{{.Fit.Reason}}</b>. Exclusions: {{range .Fit.Rejections}}{{.}}; {{else}}none in these per-event checks{{end}}.</p>
<dl><dt>Scorer observations / expected</dt><dd>{{.Fit.GeometrySamples}} / {{.Fit.ExpectedSamples}}</dd>{{if .Fit.RMSE}}<dt>Scorer ridge RMSE / allowed maximum</dt><dd>{{printf "%.3f" (number .Fit.RMSE)}} / {{printf "%.3f" (number .Fit.RMSELimit)}} Hz</dd>{{end}}
{{with .Fit.FullResolution}}<dt>Full-resolution check</dt><dd>{{.Frames}} modeled frames, {{.RidgeSamples}} ridge samples{{if .RMSE}}; {{printf "%.3f" (number .RMSE)}} Hz RMSE; {{printf "%.3f" (number .MedianAbsResidual)}} Hz median absolute residual{{end}}.</dd>{{if .MedianContrast}}<dt>Model versus nearby frequency bands</dt><dd>{{printf "%.2f" (number .MedianContrast)}} dB median contrast. Positive contrast fraction {{printf "%.3f" (number .PositiveContrastFraction)}} across {{.ContrastFrames}} frame/harmonic measurements. Descriptive only; not a confidence score.</dd>{{end}}{{end}}</dl>
{{if .Fit.Points}}<p>Time: {{printf "%.2f" .Start}}–{{printf "%.2f" .End}} s. Recorded ridge (green), flight prediction (orange). Frequency: {{printf "%.1f" .Low}}–{{printf "%.1f" .High}} Hz.</p>
<svg viewBox="0 0 1000 200" role="img" aria-label="Observed ridge and predicted frequency">{{range paths .Curve}}<path d="{{.}}"/>{{end}}</svg>
<p>Residual (observed − predicted), ±{{printf "%.2f" .MaxResidual}} Hz; center line is zero.</p><svg viewBox="0 0 1000 200" role="img" aria-label="Frequency residual"><line x1="20" y1="100" x2="980" y2="100" stroke="#64727b"/><path d="{{.Residual}}"/></svg>
<p>Implied source frequency: {{printf "%.1f" .LowSource}}–{{printf "%.1f" .HighSource}} Hz. Variation can reflect source pitch, incorrect geometry or time, or trace errors; this is not proof of engine RPM changes.</p><svg viewBox="0 0 1000 200" role="img" aria-label="Implied emitted frequency"><path d="{{.Source}}"/></svg>{{end}}</section>{{end}}</html>`
