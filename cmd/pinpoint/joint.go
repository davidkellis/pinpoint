package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"html/template"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"pinpoint/internal/acoustic"
	"pinpoint/internal/search"
)

func jointLocate(ctx context.Context, fs *flag.FlagSet, args []string) error {
	config := fs.String("config", "", "region/time/trajectory config with reviewed sound windows and source-frequency bands")
	audio := fs.String("audio", "", "recording used for fresh, audio-only curve tracing")
	out := fs.String("out", "work/measured-joint", "measured curves, prepared joint configuration and results")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *config == "" || *audio == "" || fs.NArg() != 0 {
		return fmt.Errorf("--config and --audio are required; no positional arguments")
	}
	configPath, err := filepath.Abs(*config)
	if err != nil {
		return err
	}
	// Prepared configurations must remain reusable from their output directory.
	// Resolving the input first also makes all its data paths absolute.
	c, err := readConfig(configPath)
	if err != nil {
		return err
	}
	if len(c.Events) < 2 {
		return fmt.Errorf("provide at least two reviewed flyover windows in the configuration")
	}
	dir, err := filepath.Abs(*out)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	fmt.Println("Tracing the recording at two FFT resolutions; no flight data or location enters this stage.")
	s, secondary, detection, err := acoustic.AnalyzeWithSpectra(ctx, *audio)
	if err != nil {
		return err
	}
	if err = search.JSON(filepath.Join(dir, "detection.json"), detection); err != nil {
		return err
	}
	r, traceErr := acoustic.TraceWindows(ctx, s, secondary, detection, c.Events)
	if err = search.JSON(filepath.Join(dir, "curve-traces.json"), r); err != nil {
		return err
	}
	if err = writeTraceReview(filepath.Join(dir, "curves.html"), s, secondary, r, false); err != nil {
		return err
	}
	if traceErr != nil {
		return traceErr
	}
	c, err = search.MeasuredJointConfig(c, r.Events)
	if err != nil {
		return err
	}
	c.Spectrum = filepath.Join(dir, "spectrogram.bin")
	if _, err = c.Validate(s); err != nil {
		return err
	}
	if err = s.Save(c.Spectrum); err != nil {
		return err
	}
	prepared := filepath.Join(dir, "joint-search.json")
	if err = search.JSON(prepared, c); err != nil {
		return err
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	hashes := map[string]string{}
	for _, p := range []string{*audio, configPath, c.Tracks, c.Spectrum, prepared, executable} {
		f, e := os.Open(p)
		if e != nil {
			return e
		}
		h := sha256.New()
		_, e = io.Copy(h, f)
		f.Close()
		if e != nil {
			return e
		}
		hashes[p] = hex.EncodeToString(h.Sum(nil))
	}
	if err = search.JSON(filepath.Join(dir, "provenance.json"), map[string]any{"inputs_sha256": hashes, "created_utc": time.Now().UTC().Format(time.RFC3339), "go_version": runtime.Version(), "detector": detection.Version, "note": "Reviewed time/source-frequency windows and physical limits come from the supplied configuration. Curves are measured afresh from audio. All selected events share one clock in a new joint search over the original region. No prior location results, reference GPS, or flight identities are inputs; this is nevertheless reviewed-window, not blind-event selection."}); err != nil {
		return err
	}
	fmt.Printf("%d measured curves selected. Review: %s\nSearching the original region jointly, then refining survivors.\n", len(c.Events), filepath.Join(dir, "curves.html"))
	if err = locate(ctx, c, filepath.Join(dir, "results")); err != nil {
		return err
	}
	return writeTraceReview(filepath.Join(dir, "curves.html"), s, secondary, r, true)
}

func writeTraceReview(path string, primary, secondary *acoustic.Spectrum, r acoustic.TraceReport, completed bool) error {
	type figure struct {
		Decision acoustic.TraceDecision
		Image    acoustic.SpectralImage
		Path     string
	}
	var figures []figure
	for _, d := range r.Decisions {
		f := figure{Decision: d}
		if d.Event != nil {
			s := primary
			if secondary != nil && d.Event.Resolution == float64(secondary.Window)/float64(secondary.SampleRate) {
				s = secondary
			}
			im, err := s.EventImage(*d.Event)
			if err != nil {
				return err
			}
			f.Image = im
			for i, p := range d.Event.Ridge {
				op := "L"
				if i == 0 || p.Time-d.Event.Ridge[i-1].Time > 2.5 {
					op = "M"
				}
				f.Path += fmt.Sprintf("%s%.2f,%.2f ", op, 900*(p.Time-im.Start)/(im.End-im.Start), 280*(im.MaxHz-p.Hz)/(im.MaxHz-im.MinHz))
			}
		}
		figures = append(figures, f)
	}
	t, err := template.New("traces").Funcs(template.FuncMap{"image": func(s string) template.URL { return template.URL(s) }}).Parse(`<!doctype html><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>Measured flyover curves</title><style>body{font:16px system-ui;background:#14252a;color:#e2eeee;max-width:1200px;margin:auto;padding:24px;line-height:1.5}a{color:#83d6c6}svg{width:100%}.ridge{stroke:#66ebd4;stroke-width:2;fill:none}body.hide .ridge{display:none}</style>
<h1>Measured flyover curves</h1><p>{{.Note}}</p><p>The cyan paths are measured from audio. Flight predictions are compared separately in the result map.</p><label><input type="checkbox" checked onchange="document.body.classList.toggle('hide',!this.checked)"> Show measured ridges</label><p><a href="curve-traces.json">Trace decisions and samples</a>{{if .Completed}} · <a href="joint-search.json">Joint search configuration</a> · <a href="results/map.html">Result map</a>{{else}} · Joint search has not completed for these traces.{{end}}</p>
{{range .Figures}}<h2>Reviewed window {{.Decision.Window}}</h2><p>{{.Decision.Candidates}} eligible curve(s). {{.Decision.Reason}}</p>{{if .Decision.Event}}<p>{{printf "%.1f–%.1f s" .Decision.Event.Start .Decision.Event.End}} · {{len .Decision.Event.Ridge}} measured samples · {{.Decision.Event.Resolution}} s FFT window. Partial flyover: {{.Decision.Event.Partial}}.</p><svg viewBox="0 0 900 280"><image href="{{image .Image.DataURL}}" width="900" height="280" preserveAspectRatio="none"/><path class="ridge" d="{{.Path}}"/></svg><p>Time {{printf "%.1f–%.1f s" .Image.Start .Image.End}}; frequency {{printf "%.1f–%.1f Hz" .Image.MinHz .Image.MaxHz}}.</p>{{end}}{{end}}`)
	if err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	err = t.Execute(f, struct {
		Note      string
		Figures   []figure
		Completed bool
	}{r.Note, figures, completed})
	closeErr := f.Close()
	if err != nil {
		return err
	}
	return closeErr
}
