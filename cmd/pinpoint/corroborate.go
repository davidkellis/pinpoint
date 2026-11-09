package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"pinpoint/internal/acoustic"
	"pinpoint/internal/search"
	"pinpoint/internal/tracks"
)

func corroborate(ctx context.Context, fs *flag.FlagSet, args []string) error {
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") && !strings.HasPrefix(arg, "--") {
			return fmt.Errorf("use two-hyphen options, for example --config search.json")
		}
	}
	config := fs.String("config", "", "original search configuration")
	results := fs.String("results", "", "original results.json containing seed locations and clocks")
	out := fs.String("out", "work/corroboration", "separate follow-up report directory")
	o := search.DefaultGuidedOptions()
	fs.Float64Var(&o.MinHz, "min-hz", o.MinHz, "minimum source tone frequency")
	fs.Float64Var(&o.MaxHz, "max-hz", o.MaxHz, "maximum source tone frequency (within available spectral band)")
	fs.Float64Var(&o.StepHz, "step-hz", o.StepHz, "source tone frequency step")
	fs.Float64Var(&o.MaxSlant, "max-slant", o.MaxSlant, "maximum candidate flight slant range in metres")
	fs.IntVar(&o.MaxCandidates, "max-candidates", o.MaxCandidates, "maximum seeded hypotheses to check (1–256)")
	fs.IntVar(&o.MaxPasses, "max-passes", o.MaxPasses, "maximum additional flight encounters per seed (1–2000)")
	fs.IntVar(&o.Workers, "workers", o.Workers, "parallel seed workers (1–32)")
	fs.IntVar(&o.SequenceControls, "sequence-controls", 0, "whole-search wrong-time trials (0 disables; 5–39, e.g. 19)")
	fs.IntVar(&o.SequenceExtra, "sequence-extra", o.SequenceExtra, "additional distinct flyovers required per sequence (1–4)")
	fs.BoolVar(&o.FullCurves, "full-curves", false, "experimental: require 120-second windows rather than also testing 40-second fragments")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *config == "" || *results == "" || fs.NArg() != 0 {
		return fmt.Errorf("--config and --results are required; no positional arguments")
	}
	c, err := readConfig(*config)
	if err != nil {
		return err
	}
	s, err := acoustic.Load(c.Spectrum)
	if err != nil {
		return err
	}
	f, err := os.Open(c.Tracks)
	if err != nil {
		return err
	}
	ts, err := tracks.Read(f)
	f.Close()
	if err != nil {
		return err
	}
	b, err := os.ReadFile(*results)
	if err != nil {
		return err
	}
	var prior search.Report
	if err = json.Unmarshal(b, &prior); err != nil {
		return err
	}
	seeds := append(append([]search.Candidate(nil), prior.Candidates...), prior.FlightGroups...)
	if o.SequenceControls > 0 {
		seeds = append(seeds, prior.Hypotheses...)
	}
	r, err := search.Corroborate(ctx, c, s, ts, seeds, o, func(done, total int) {
		if done == 1 || done%10 == 0 || done == total {
			fmt.Fprintf(os.Stderr, "Completed candidate/control check %d of %d\n", done, total)
		}
	})
	if err != nil {
		return err
	}
	r.SeedNote = prior.Note
	if err = search.WriteGuidedReport(*out, r, s); err != nil {
		return err
	}
	hashes := map[string]string{}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	for _, p := range []string{*config, *results, c.Spectrum, c.Tracks, executable} {
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
	if err = search.JSON(filepath.Join(*out, "provenance.json"), map[string]any{"inputs_sha256": hashes, "created_utc": time.Now().UTC().Format(time.RFC3339), "go_version": runtime.Version(), "options": o, "config": c, "seed_search_note": prior.Note, "note": "Seed hypotheses are reused at fixed coordinates/clocks. Original search outputs and regression labels are unchanged. Candidate coordinates/clocks come from supplied results, whose prior selection may be reference-assisted."}); err != nil {
		return err
	}
	supported := 0
	for _, p := range r.Candidates {
		if p.Supported > 0 {
			supported++
		}
	}
	fmt.Printf("%d of %d seed hypotheses have additional support under experimental checks.\nReport: %s\n", supported, len(r.Candidates), filepath.Join(*out, "guided.html"))
	if r.Sequence != nil {
		fmt.Printf("Sequence audit: %s (not a calibrated location confidence).\n", r.Sequence.Conclusion)
	}
	return nil
}
