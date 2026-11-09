package main

import (
	"bufio"
	"compress/gzip"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"pinpoint/internal/acoustic"
	"pinpoint/internal/geo"
	"pinpoint/internal/search"
	"pinpoint/internal/tracks"
)

func parseBounds(s string) (geo.Bounds, error) {
	parts := strings.Split(s, ",")
	if len(parts) != 4 {
		return geo.Bounds{}, fmt.Errorf("bounds requires south,west,north,east")
	}
	var v [4]float64
	for i, p := range parts {
		x, err := strconv.ParseFloat(strings.TrimSpace(p), 64)
		if err != nil {
			return geo.Bounds{}, err
		}
		v[i] = x
	}
	b := geo.Bounds{South: v[0], West: v[1], North: v[2], East: v[3]}
	if !b.Valid() {
		return b, fmt.Errorf("invalid bounds")
	}
	return b, nil
}

func consensusLocate(ctx context.Context, fs *flag.FlagSet, args []string) error {
	d := search.DefaultConsensusOptions()
	audio := fs.String("audio", "", "recording to analyze afresh (or use --detection)")
	detectionPath := fs.String("detection", "", "existing detection.json to reuse instead of --audio")
	profile := fs.String("profile", acoustic.HybridProfile, "acoustic profile for --audio: standard, short-pass or hybrid")
	trackPath := fs.String("tracks", "", "imported flight tracks CSV covering the recording")
	start := fs.String("start", "", "approximate recording start, RFC3339 with time zone")
	state := fs.String("state", "", "two-letter state to search (polygon mask)")
	box := fs.String("bounds", "", "south,west,north,east (instead of, or within, --state)")
	terrainDir := fs.String("terrain-dir", "work/web/cache/terrain", "SRTM .hgt tile directory")
	geoidPath := fs.String("geoid", "work/web/cache/egm96_15.gtx", "EGM96 geoid grid")
	download := fs.Bool("download-terrain", false, "download missing SRTM tiles")
	mic := fs.Float64("mic-height", 1.5, "microphone height above terrain, metres")
	maxHz := fs.Float64("max-hz", acoustic.DefaultLocatableMaxHz, "ignore curves whose median pitch is above this")
	maxCurves := fs.Int("max-curves", 16, "keep at most this many (longest) strong curves")
	fs.IntVar(&d.MinCurves, "min-curves", d.MinCurves, "curves that must agree on one location and clock (1 = single-curve mode)")
	fs.Float64Var(&d.Criteria.MaxNormalized, "max-normalized", d.Criteria.MaxNormalized, "largest normalized training residual an explained curve may have")
	fs.Float64Var(&d.Criteria.MaxUnexplained, "max-unexplained", d.Criteria.MaxUnexplained, "largest fraction of a curve's bend (Doppler/straight-line residual) an explaining aircraft may leave")
	fs.Float64Var(&d.Criteria.MinSwingHz, "min-swing", d.Criteria.MinSwingHz, "minimum pitch swing (constant-model residual, Hz) for a curve to count")
	fs.Float64Var(&d.Grid, "grid", d.Grid, "coarse grid metres")
	fs.Float64Var(&d.Refine, "refine", d.Refine, "refinement grid metres")
	fs.Float64Var(&d.Final, "final-grid", d.Final, "final grid metres")
	fs.IntVar(&d.RefinePeaks, "refine-peaks", d.RefinePeaks, "separated coarse seeds to refine")
	fs.IntVar(&d.FinalPeaks, "final-peaks", d.FinalPeaks, "separated refined seeds to refine again")
	fs.Float64Var(&d.ClockWindow, "clock-window", d.ClockWindow, "recording-start uncertainty, +/- seconds")
	fs.Float64Var(&d.ClockStep, "clock-step", d.ClockStep, "coarse clock step seconds")
	fs.Float64Var(&d.FineClockStep, "fine-clock-step", 0, "clock step around seeds during refinement (default: clock step)")
	fs.Float64Var(&d.ResolveRatio, "resolve-ratio", d.ResolveRatio, "runner-up/winner residual ratio required to call a result resolved")
	fs.Float64Var(&d.MaxSourceChange, "max-source-change", 0, "bounded fractional source-pitch drift, 0..0.25")
	fs.IntVar(&d.Workers, "workers", max(1, runtime.NumCPU()-1), "parallel receiver workers")
	smoothStep := fs.Float64("smooth-track-step", .25, "Hermite track interpolation step seconds (0 disables)")
	reported := fs.Bool("reported-motion", true, "use reported ground speed/track/climb in interpolation when present")
	reconstruct := fs.Bool("reconstruct-track", true, "Kalman/RTS reconstruction before interpolation (requires reported motion)")
	fs.IntVar(&d.Regions, "regions", d.Regions, "separated regions to report")
	loud := fs.String("loudness-audio", "", "recording used for the close-pass loudness check (default: --audio)")
	out := fs.String("out", "work/consensus", "new output directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if (*audio == "") == (*detectionPath == "") || *trackPath == "" || *start == "" || (*state == "" && *box == "") || fs.NArg() != 0 {
		return fmt.Errorf("supply exactly one of --audio/--detection, plus --tracks, --start and --state or --bounds")
	}
	if *reconstruct && (!*reported || *smoothStep == 0) {
		return fmt.Errorf("--reconstruct-track requires --reported-motion and --smooth-track-step")
	}
	d.RecordingStart = *start
	d.RegionMask = strings.ToUpper(*state)
	if *state != "" {
		b, ok := geo.StateBounds(d.RegionMask)
		if !ok {
			return fmt.Errorf("unknown state %q", *state)
		}
		d.Bounds = b
	}
	if *box != "" {
		b, err := parseBounds(*box)
		if err != nil {
			return err
		}
		d.Bounds = b
	}
	if _, err := os.Stat(filepath.Join(*out, "consensus.json")); err == nil {
		return fmt.Errorf("%s already holds a result; choose a new --out", *out)
	}
	if err := os.MkdirAll(*out, 0755); err != nil {
		return err
	}
	var report acoustic.DetectionReport
	if *audio != "" {
		var s *acoustic.Spectrum
		var err error
		switch *profile {
		case "standard":
			s, report, err = acoustic.AnalyzeMultiResolution(ctx, *audio)
		case acoustic.ShortPassProfile:
			s, report, err = acoustic.AnalyzeShortPass(ctx, *audio, acoustic.ShortPassOptions())
		case acoustic.HybridProfile:
			s, report, err = acoustic.AnalyzeHybrid(ctx, *audio)
		default:
			return fmt.Errorf("unknown detector profile")
		}
		if err != nil {
			return err
		}
		if err = s.Save(filepath.Join(*out, "spectrogram.bin")); err != nil {
			return err
		}
		if err = search.JSON(filepath.Join(*out, "detection.json"), report); err != nil {
			return err
		}
	} else {
		b, err := os.ReadFile(*detectionPath)
		if err != nil {
			return err
		}
		if err = json.Unmarshal(b, &report); err != nil {
			return err
		}
	}
	events, curves := acoustic.LocatableCurves(report.Events, *maxHz, *maxCurves)
	fmt.Printf("Selected %d of %d detector proposals (strong, measured, median pitch <= %.0f Hz)\n", len(curves), len(report.Events), *maxHz)
	for _, c := range curves {
		fmt.Printf("  curve %d: %.1f-%.1f s, median %.0f Hz\n", c.Index, c.Start, c.End, c.MedianHz)
	}
	extra := map[string]any{"audio": *audio, "detection": *detectionPath, "tracks": *trackPath, "max_median_hz": *maxHz,
		"receiver_truth": "not supplied; compare any ground truth only after this result is written"}
	if len(events) < d.MinCurves {
		res := search.ConsensusResult{Options: d, Curves: len(events), Status: "too_few_curves", Note: "Fewer usable curves than --min-curves; no location search was run."}
		fmt.Println("Too few usable curves for consensus; no search run.")
		return search.WriteConsensusResult(*out, res, curves, extra)
	}
	f, err := os.Open(*trackPath)
	if err != nil {
		return err
	}
	ts, err := tracks.Read(f)
	f.Close()
	if err != nil {
		return err
	}
	model := map[string]any{"smooth_track_step_s": *smoothStep, "reported_motion": *reported, "reconstruct_track": *reconstruct}
	if *smoothStep > 0 {
		for i := range ts {
			if *reconstruct {
				if ts[i], _, err = ts[i].Reconstruct(tracks.DefaultReconstructionOptions()); err != nil {
					return err
				}
			}
			if ts[i], _, err = ts[i].Smooth(tracks.SmoothingOptions{Step: *smoothStep, MaxGap: 30, MaxAcceleration: 20, UseKinematics: *reported}); err != nil {
				return err
			}
		}
	}
	heights, err := geo.NewHeightSampler(*terrainDir, *geoidPath, *mic, *download)
	if err != nil {
		return err
	}
	d.Height = heights.Func(ctx)
	if *loud == "" {
		*loud = *audio
	}
	if *loud != "" {
		l, err := acoustic.LoudnessEnvelope(ctx, *loud, 0, 50, 2000, 1)
		if err != nil {
			return err
		}
		d.Loudness = &l
		extra["loudness_audio"] = *loud
	}
	last := time.Time{}
	d.Progress = func(stage string, done, total int, best float64) {
		if done != total && time.Since(last) < 5*time.Second {
			return
		}
		last = time.Now()
		fmt.Printf("%s: %d/%d points, best worst-normalized residual %.3f\n", stage, done, total, best)
	}
	res, err := search.Consensus(ctx, events, ts, d)
	if err != nil {
		return err
	}
	// Report curve numbers as detector indices, not positions in the selection.
	for i := range res.Regions {
		for j := range res.Regions[i].Matches {
			res.Regions[i].Matches[j].Event = curves[res.Regions[i].Matches[j].Event-1].Index
		}
	}
	extra["track_model"] = model
	extra["height_note"] = "SRTM terrain + EGM96 geoid + microphone height"
	extra["terrain_voids_set_to_sea_level"] = heights.Voids()
	if err = search.WriteConsensusResult(*out, res, curves, extra); err != nil {
		return err
	}
	if err = writeConsensusPoints(filepath.Join(*out, "points.jsonl.gz"), res.Points); err != nil {
		return err
	}
	if err = writeRegionsGeoJSON(filepath.Join(*out, "regions.geojson"), res.Regions); err != nil {
		return err
	}
	fmt.Printf("Status: %s after %d points in %.0f s\n", res.Status, res.Evaluated, res.Elapsed)
	for i, r := range res.Regions {
		fmt.Printf("  region %d: %.6f, %.6f  clock %+.1f s  unexplained %.3f  curves %d", i+1, r.Lat, r.Lon, r.Clock, r.Worst, r.Explains)
		if r.Loudness != nil {
			fmt.Printf("  loudness z %.2f (rho %.2f, %d passes, %d close-but-quiet)", r.Loudness.Z, r.Loudness.Spearman, r.Loudness.Passes, r.Loudness.Violations)
		}
		fmt.Println()
	}
	return nil
}

func writeConsensusPoints(path string, points []search.ConsensusPoint) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	z := gzip.NewWriter(f)
	w := bufio.NewWriter(z)
	enc := json.NewEncoder(w)
	for _, p := range points {
		row := map[string]any{"lat": p.Lat, "lon": p.Lon, "grid_m": p.Spacing, "valid": p.Valid}
		if p.Valid {
			row["worst"], row["clock_s"], row["curves"] = p.Worst, p.Clock, p.Explains
		}
		if err = enc.Encode(row); err != nil {
			return err
		}
	}
	if err = w.Flush(); err != nil {
		return err
	}
	return z.Close()
}

func writeRegionsGeoJSON(path string, regions []search.ConsensusPoint) error {
	type feature struct {
		Type       string         `json:"type"`
		Geometry   map[string]any `json:"geometry"`
		Properties map[string]any `json:"properties"`
	}
	fc := struct {
		Type     string    `json:"type"`
		Features []feature `json:"features"`
	}{Type: "FeatureCollection", Features: []feature{}}
	for i, r := range regions {
		var aircraft []string
		for _, m := range r.Matches {
			aircraft = append(aircraft, fmt.Sprintf("curve %d: %s", m.Event, m.Aircraft))
		}
		fc.Features = append(fc.Features, feature{Type: "Feature", Geometry: map[string]any{"type": "Point", "coordinates": []float64{r.Lon, r.Lat}},
			Properties: map[string]any{"rank": i + 1, "worst_unexplained_curvature": r.Worst, "clock_s": r.Clock, "curves_explained": r.Explains, "aircraft": aircraft}})
	}
	return search.JSON(path, fc)
}
