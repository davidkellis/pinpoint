//go:build ignore

// Merge completed bands from one search into a map with global rankings.
// Refuses incompatible configurations and missing band results.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"pinpoint/internal/acoustic"
	"pinpoint/internal/geo"
	"pinpoint/internal/reference"
	"pinpoint/internal/search"
	"pinpoint/internal/tracks"
	"sort"
	"strings"
)

func main() {
	input := flag.String("input", "", "directory with band-*.json and band-*/results.json")
	out := flag.String("out", "", "combined report directory")
	flag.Parse()
	if *input == "" || *out == "" {
		panic("required: --input and --out")
	}
	paths, err := filepath.Glob(filepath.Join(*input, "band-*.json"))
	must(err)
	if len(paths) == 0 {
		panic("no search bands")
	}
	var cfg search.Config
	var result search.Report
	var signature string
	for i, path := range paths {
		var c search.Config
		must(reference.ReadJSON(path, &c))
		var r search.Report
		must(reference.ReadJSON(filepath.Join(*input, strings.TrimSuffix(filepath.Base(path), ".json"), "results.json"), &r))
		if r.Bounds != c.Bounds {
			panic("result/config bounds differ")
		}
		plain := c
		plain.Bounds = geo.Bounds{}
		b, err := json.Marshal(plain)
		must(err)
		if i == 0 {
			cfg = c
			signature = string(b)
			result = search.Report{Status: "no_match", Bounds: c.Bounds, Grid: r.Grid, Refine: r.Refine, FinalGrid: r.FinalGrid, MinScore: r.MinScore, Note: "Combined completed search bands. Scores are heuristic evidence, not probabilities. Recording city is an evaluation label only; exact microphone GPS is unpublished."}
		} else if string(b) != signature {
			panic("search bands have different inputs or scoring settings")
		}
		result.Bounds.South = min(result.Bounds.South, c.Bounds.South)
		result.Bounds.North = max(result.Bounds.North, c.Bounds.North)
		result.Bounds.West = min(result.Bounds.West, c.Bounds.West)
		result.Bounds.East = max(result.Bounds.East, c.Bounds.East)
		result.Evaluated += r.Evaluated
		result.FlightGroups = append(result.FlightGroups, r.FlightGroups...)
		result.ScoreGrid = append(result.ScoreGrid, r.ScoreGrid...)
		result.RefinementPasses = append(result.RefinementPasses, r.RefinementPasses...)
	}
	sort.SliceStable(result.FlightGroups, func(i, j int) bool { return result.FlightGroups[i].Score > result.FlightGroups[j].Score })
	unique := []search.Candidate{}
	seen := map[string]bool{}
	for _, p := range result.FlightGroups {
		ids := []string{}
		for _, e := range p.Events {
			ids = append(ids, e.Track)
		}
		key := strings.Join(ids, "/")
		if !seen[key] {
			seen[key] = true
			unique = append(unique, p)
		}
	}
	result.FlightGroups = unique
	result.Candidates = unique[:min(20, len(unique))]
	if len(unique) > 0 {
		result.Status = "candidate_matches"
	}
	search.AssessAmbiguity(&result)
	cfg.Bounds = result.Bounds
	s, err := acoustic.Load(cfg.Spectrum)
	must(err)
	f, err := os.Open(cfg.Tracks)
	must(err)
	ts, err := tracks.Read(f)
	f.Close()
	must(err)
	must(search.WriteReport(*out, result, cfg, s, ts))
	fmt.Printf("%d completed bands, %d locations, %d flight groups; %s/map.html\n", len(paths), result.Evaluated, len(unique), *out)
}
func must(err error) {
	if err != nil {
		panic(err)
	}
}
