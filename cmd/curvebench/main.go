package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"

	"pinpoint/internal/curvebench"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	fs := flag.NewFlagSet("curvebench", flag.ContinueOnError)
	config := fs.String("config", "examples/curve-experiments.json", "experiment manifest")
	out := fs.String("out", "work/curve-experiments", "report directory")
	workers := fs.Int("workers", 2, "independent cases in parallel (1–4)")
	only := fs.String("only", "", "comma-separated case IDs (optional)")
	methods := fs.String("methods", "", "comma-separated methods (default: all nine)")
	render := fs.Bool("render-only", false, "rebuild reports/evaluation from saved spectra displays and detections; no DSP")
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "Usage: just experiment [--config manifest.json] [--out directory] [--workers 2] [--only case-id] [--methods method,method] [--render-only]")
		fs.VisitAll(func(f *flag.Flag) { fmt.Fprintf(fs.Output(), "  --%s: %s (default %s)\n", f.Name, f.Usage, f.DefValue) })
	}
	for _, arg := range os.Args[1:] {
		if strings.HasPrefix(arg, "-") && !strings.HasPrefix(arg, "--") {
			return fmt.Errorf("use two-hyphen options, such as --config")
		}
	}
	if err := fs.Parse(os.Args[1:]); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments")
	}
	if *render {
		return curvebench.Rebuild(*out)
	}
	f, err := os.Open(*config)
	if err != nil {
		return err
	}
	defer f.Close()
	var c curvebench.Config
	d := json.NewDecoder(f)
	d.DisallowUnknownFields()
	if err = d.Decode(&c); err != nil {
		return err
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return fmt.Errorf("expected one manifest")
	}
	if *methods != "" {
		c.Methods = strings.Split(*methods, ",")
	}
	if *only != "" {
		wanted := map[string]bool{}
		for _, id := range strings.Split(*only, ",") {
			wanted[id] = true
		}
		var selected []curvebench.Case
		for _, x := range c.Cases {
			if wanted[x.ID] {
				selected = append(selected, x)
				delete(wanted, x.ID)
			}
		}
		if len(wanted) > 0 {
			return fmt.Errorf("unknown case IDs: %v", wanted)
		}
		c.Cases = selected
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	_, err = curvebench.Run(ctx, c, *out, *workers, func(s string) { fmt.Println(s) })
	if err != nil {
		return err
	}
	fmt.Println("Comparison report:", *out+"/index.html")
	return nil
}
