//go:build ignore

// Regional, training-ranked matched-filter study without an acoustic proposal.
package main

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"pinpoint/internal/acoustic"
	"pinpoint/internal/geo"
	"pinpoint/internal/search"
	"pinpoint/internal/tracks"
)

type regionPoint struct {
	Lat        float64                  `json:"lat"`
	Lon        float64                  `json:"lon"`
	Height     float64                  `json:"height_ellipsoid_m"`
	Spacing    float64                  `json:"grid_m"`
	Clock      float64                  `json:"shared_clock_correction_s"`
	Score      float64                  `json:"weakest_training_selection_db,omitempty"`
	Rank       float64                  `json:"training_rank"`
	Metric     string                   `json:"ranking_metric"`
	Ridges     []search.JointRidgeMatch `json:"ridge_matches,omitempty"`
	Valid      bool                     `json:"model_available"`
	Pass       bool                     `json:"all_reserved_checks_pass"`
	Matches    []search.GuidedMatch     `json:"training_selected_matches"`
	Validation string                   `json:"validation_status,omitempty"`
}

func fail(e error) {
	if e != nil {
		panic(e)
	}
}
func writeJSON(p string, v any) {
	b, e := json.MarshalIndent(v, "", "  ")
	fail(e)
	fail(os.WriteFile(p, append(b, '\n'), 0600))
}

func displayRegionPoints(all, peaks []regionPoint, limit int, coarse float64) []regionPoint {
	if len(all) <= limit {
		return all
	}
	var result []regionPoint
	seen := map[[2]int64]bool{}
	add := func(p regionPoint) {
		key := [2]int64{int64(math.Round(p.Lat * 1e8)), int64(math.Round(p.Lon * 1e8))}
		if !seen[key] && len(result) < limit {
			seen[key] = true
			result = append(result, p)
		}
	}
	for _, p := range peaks {
		add(p)
	}
	var refined, initial []regionPoint
	for _, p := range all {
		if p.Spacing < coarse {
			refined = append(refined, p)
		} else {
			initial = append(initial, p)
		}
	}
	stride := max(1, int(math.Ceil(float64(len(refined))/float64(max(1, limit*3/5)))))
	for i := 0; i < len(refined); i += stride {
		add(refined[i])
	}
	stride = max(1, int(math.Ceil(float64(len(initial))/float64(max(1, limit-len(result))))))
	for i := 0; i < len(initial); i += stride {
		add(initial[i])
	}
	return result
}
func identity(id string) string {
	parts := strings.Split(id, "-")
	if len(parts) == 3 && len(parts[0]) == 6 {
		return strings.ToLower(parts[0])
	}
	return id
}

// Candidate buckets conservatively enclose the complete sampled trajectory plus
// the acoustic radius. They remove geographically impossible work, not aircraft
// identities. All buckets are immutable before receiver workers start.
func regionTrackIndex(ts []tracks.Track, radius float64) func(geo.Vec) []tracks.Track {
	const cell = 25000.
	index := map[[3]int][]tracks.Track{}
	for _, t := range ts {
		if len(t.Points) < 2 {
			continue
		}
		lo := geo.Vec{X: math.Inf(1), Y: math.Inf(1), Z: math.Inf(1)}
		hi := geo.Vec{X: math.Inf(-1), Y: math.Inf(-1), Z: math.Inf(-1)}
		for _, p := range t.Points {
			lo.X = math.Min(lo.X, p.Position.X)
			lo.Y = math.Min(lo.Y, p.Position.Y)
			lo.Z = math.Min(lo.Z, p.Position.Z)
			hi.X = math.Max(hi.X, p.Position.X)
			hi.Y = math.Max(hi.Y, p.Position.Y)
			hi.Z = math.Max(hi.Z, p.Position.Z)
		}
		for x := int(math.Floor((lo.X - radius) / cell)); x <= int(math.Floor((hi.X+radius)/cell)); x++ {
			for y := int(math.Floor((lo.Y - radius) / cell)); y <= int(math.Floor((hi.Y+radius)/cell)); y++ {
				for z := int(math.Floor((lo.Z - radius) / cell)); z <= int(math.Floor((hi.Z+radius)/cell)); z++ {
					key := [3]int{x, y, z}
					index[key] = append(index[key], t)
				}
			}
		}
	}
	return func(p geo.Vec) []tracks.Track {
		return index[[3]int{int(math.Floor(p.X / cell)), int(math.Floor(p.Y / cell)), int(math.Floor(p.Z / cell))}]
	}
}

// Screen whole segments conservatively before interpolation; do not select a
// flight identity or replace geometry with a chord between disjoint windows.
// The 25 km spatial and 200 s temporal margins exceed the 15 km acoustic gate.
func trimRegionTracks(ts []tracks.Track, b geo.Bounds, events []acoustic.Event, epoch, clock float64) []tracks.Track {
	dy := 25000. / 110000
	dx := dy / math.Cos(math.Max(math.Abs(b.South), math.Abs(b.North))*math.Pi/180)
	var out []tracks.Track
	for _, t := range ts {
		first, last := len(t.Points), -1
		for i := 0; i+1 < len(t.Points); i++ {
			a, z := t.Points[i], t.Points[i+1]
			if math.Max(a.Lat, z.Lat) < b.South-dy || math.Min(a.Lat, z.Lat) > b.North+dy || math.Max(a.Lon, z.Lon) < b.West-dx || math.Min(a.Lon, z.Lon) > b.East+dx {
				continue
			}
			for _, e := range events {
				if z.Time >= epoch+e.Start-200-clock && a.Time <= epoch+e.End+200+clock {
					first = min(first, i)
					last = max(last, i+1)
				}
			}
		}
		if last <= first {
			continue
		}
		t.Points = append([]tracks.Point(nil), t.Points[first:last+1]...)
		var motion []tracks.KinematicPoint
		for _, m := range t.Motion {
			if m.Time >= t.Points[0].Time && m.Time <= t.Points[len(t.Points)-1].Time {
				motion = append(motion, m)
			}
		}
		t.Motion = motion
		out = append(out, t)
	}
	return out
}

func ridgePreview(s *acoustic.Spectrum, path string, observed acoustic.Event, predicted []acoustic.RidgeSample) {
	lo, hi := math.Inf(1), 0.
	curves := []acoustic.Curve{{Label: "Flight forward model; training-selected receiver, clock and source pitch"}, {Label: "Measured audio-selected ridge; observations can already use the whole audio"}}
	for i, points := range [][]acoustic.RidgeSample{predicted, observed.Ridge} {
		for _, p := range points {
			lo = math.Min(lo, p.Hz)
			hi = math.Max(hi, p.Hz)
			curves[i].Points = append(curves[i].Points, [2]float64{p.Time, p.Hz})
		}
	}
	if !geo.Finite(lo) || hi <= 0 {
		return
	}
	padding := math.Max(5, (hi-lo)*.15)
	df := float64(s.SampleRate) / float64(s.Window)
	x0 := max(0, int(math.Floor((observed.Start-s.Time(0))/s.Step())))
	x1 := min(len(s.Rows)-1, int(math.Ceil((observed.End-s.Time(0))/s.Step())))
	y0 := max(0, int(math.Floor((lo-padding-s.Hz(0))/df)))
	y1 := min(len(s.Rows[0])-1, int(math.Ceil((hi+padding-s.Hz(0))/df)))
	if x1 <= x0 || y1 <= y0 {
		return
	}
	crop := *s
	crop.TimeOffset += float64(x0) * s.Step()
	crop.FirstBin += y0
	crop.Rows = make([][]float32, x1-x0+1)
	for i := range crop.Rows {
		crop.Rows[i] = s.Rows[x0+i][y0 : y1+1]
	}
	fail(crop.SVG(path, curves))
}

func main() {
	started := time.Now()
	for _, a := range os.Args[1:] {
		if strings.HasPrefix(a, "-") && !strings.HasPrefix(a, "--") {
			panic("use two-hyphen options")
		}
	}
	config := flag.String("config", "", "recording/terrain configuration; reference lat/lon are ignored")
	record := flag.String("recording", "", "one configured recording ID")
	spectrum := flag.String("spectrum", "", "measured whole-file spectrum to score")
	box := flag.String("bounds", "", "south,west,north,east of an explicit regional search")
	state := flag.String("state", "", "optional full state polygon/bounds; mutually exclusive with --bounds")
	corridors := flag.Bool("corridors", false, "coarse grid covers every eligible flight corridor during the observed intervals")
	indexed := flag.Bool("spatial-index", false, "use conservative immutable aircraft spatial buckets")
	refinePeaks := flag.Int("refine-peaks", 3, "separated coarse seeds to refine, 1..50")
	finalPeaks := flag.Int("final-peaks", 2, "separated refined seeds to refine again, 1..50")
	diverse := flag.Bool("diverse-flights", false, "retain separated winning flight combinations before filling other refinement seeds")
	deferValidation := flag.Bool("defer-validation", false, "raw mode: perform costly confirmation only after freezing training-selected locations")
	compressPoints := flag.Bool("compress-points", false, "save every evaluation in points.jsonl.gz and a smaller map sample in scan.json")
	mapLimit := flag.Int("map-limit", 25000, "maximum map display points when --compress-points is used")
	downloadTerrain := flag.Bool("download-terrain", false, "download missing required SRTM tiles; never substitute a flat elevation")
	out := flag.String("out", "work/receiver-region-study", "new output directory")
	grid := flag.Float64("grid", 250, "coarse grid metres")
	refine := flag.Float64("refine", 50, "grid metres around separated coarse peaks")
	final := flag.Float64("final-grid", 10, "final grid metres around separated refined peaks")
	clockWindow := flag.Float64("clock-window", 2, "shared clock uncertainty half width in seconds")
	clockStep := flag.Float64("clock-step", 2, "shared clock step in seconds")
	workers := flag.Int("workers", 4, "bounded independent receiver workers, 1..8")
	minimum := flag.Int("min-flights", 1, "required distinct nonoverlapping training-selected encounters, 1..4")
	maxPoints := flag.Int("max-points", 3000, "hard cap on all grid evaluations")
	smoothStep := flag.Float64("smooth-track-step", 0, "experimental telemetry Hermite interpolation: 0 disables, 0.05..2 seconds")
	reviewWindows := flag.String("review-windows", "", "optional JSON array of audio-selected start_s/end_s intervals; full audio remains available for controls")
	sourceStep := flag.Float64("source-step", 2, "source-pitch search step in Hz")
	ridgePath := flag.String("ridge-events", "", "JSON of complete audio-selected curves; ranks full-curve training residuals instead of spectral fragments")
	reportedMotion := flag.Bool("reported-motion", false, "use retained ground speed/true track/geometric rate in optional Hermite interpolation")
	reconstruct := flag.Bool("reconstruct-track", false, "experimental fixed-uncertainty position/velocity Kalman + RTS reconstruction before Hermite interpolation; requires --reported-motion")
	maxSourceChange := flag.Float64("max-source-change", 0, "bounded fractional source-pitch variation, 0..0.25")
	polish := flag.Bool("polish-ridges", false, "refine strong measured ridges using local audio peaks only, before any receiver or flight testing")
	harmonicSamples := flag.Int("measure-ridge-harmonics", 0, "experimental local peak sampling with 1..8 harmonics; 0 leaves observed ridges unchanged")
	flag.Parse()
	if *mapLimit < 100 || *mapLimit > 100000 {
		panic("--map-limit must be 100..100000")
	}
	if *reportedMotion && *smoothStep == 0 {
		panic("--reported-motion requires --smooth-track-step")
	}
	if *reconstruct && !*reportedMotion {
		panic("--reconstruct-track requires --reported-motion and --smooth-track-step")
	}
	if !geo.Finite(*maxSourceChange) || *maxSourceChange < 0 || *maxSourceChange > .25 || !geo.Finite(*sourceStep) || *sourceStep <= 0 {
		panic("invalid source limits")
	}
	for _, v := range []float64{*grid, *refine, *final, *clockWindow, *clockStep} {
		if !geo.Finite(v) {
			panic("nonfinite scan limit")
		}
	}
	if *config == "" || *record == "" || *spectrum == "" || *workers < 1 || *workers > 8 || *minimum < 1 || *minimum > 4 || *maxPoints < 1 || *maxPoints > 2000000 || *refinePeaks < 1 || *refinePeaks > 50 || *finalPeaks < 1 || *finalPeaks > 50 || *grid < 10 || *refine < 10 || *refine > *grid || *final < 5 || *final > *refine || *clockWindow < 0 || *clockWindow > 30 || *clockStep <= 0 || 2**clockWindow / *clockStep > 60 {
		panic("invalid regional scan limits")
	}
	if !geo.Finite(*smoothStep) || *smoothStep != 0 && (*smoothStep < .05 || *smoothStep > 2) {
		panic("invalid smoothing step")
	}
	if *state != "" {
		if *box != "" {
			panic("use --state or --bounds")
		}
		b, ok := geo.StateBounds(*state)
		if !ok {
			panic("unknown state")
		}
		*box = fmt.Sprintf("%.9f,%.9f,%.9f,%.9f", b.South, b.West, b.North, b.East)
	}
	parts := strings.Split(*box, ",")
	if len(parts) != 4 {
		panic("bounds requires south,west,north,east")
	}
	var coords [4]float64
	for i, p := range parts {
		v, e := strconv.ParseFloat(p, 64)
		fail(e)
		coords[i] = v
	}
	bounds := geo.Bounds{South: coords[0], West: coords[1], North: coords[2], East: coords[3]}
	if !bounds.Valid() {
		panic("invalid bounds")
	}
	b, e := os.ReadFile(*config)
	fail(e)
	var cfg struct {
		Terrain    string                                      `json:"terrain_dir"`
		Geoid      string                                      `json:"geoid_file"`
		Recordings []struct{ ID, Start, Tracks, Audio string } `json:"recordings"`
	}
	fail(json.Unmarshal(b, &cfg))
	start, trackPath, audio := "", "", ""
	for _, r := range cfg.Recordings {
		if r.ID == *record {
			start = r.Start
			trackPath = r.Tracks
			audio = r.Audio
		}
	}
	if start == "" {
		panic("recording not configured")
	}
	s, e := acoustic.Load(*spectrum)
	fail(e)
	f, e := os.Open(trackPath)
	fail(e)
	ts, e := tracks.Read(f)
	f.Close()
	fail(e)
	if *polish && *ridgePath == "" {
		panic("--polish-ridges requires --ridge-events")
	}
	var measurements []acoustic.RidgeSampling
	if *harmonicSamples < 0 || *harmonicSamples > 8 || *harmonicSamples > 0 && *ridgePath == "" {
		panic("invalid --measure-ridge-harmonics")
	}
	var events []acoustic.Event
	var reviewIntervals []search.ReviewInterval
	if *reviewWindows != "" {
		b, e := os.ReadFile(*reviewWindows)
		fail(e)
		fail(json.Unmarshal(b, &reviewIntervals))
	}
	if *ridgePath != "" {
		b, e := os.ReadFile(*ridgePath)
		fail(e)
		fail(json.Unmarshal(b, &events))
		if len(events) < 1 || len(events) > 8 {
			panic("requires 1..8 complete observed curves")
		}
		if *minimum > len(events) {
			panic("--min-flights exceeds supplied distinct observed curves")
		}
		*minimum = len(events) // every supplied curve is mandatory in ridge mode

		if *polish {
			for i := range events {
				refined, ok := acoustic.RefineMeasuredCurve(s, nil, events[i])
				if ok {
					events[i] = refined
				}
				fmt.Printf("Audio-only ridge %d: sub-bin refinement used %v, %d observations\n", i+1, ok, len(events[i].Ridge))
			}
		}
		if *harmonicSamples > 0 {
			for i := range events {
				measured, report, e := acoustic.MeasureRidgeSamples(s, events[i], *harmonicSamples)
				fail(e)
				events[i] = measured
				measurements = append(measurements, report)
			}
		}
		epoch, e := time.Parse(time.RFC3339Nano, start)
		fail(e)
		ts = trimRegionTracks(ts, bounds, events, float64(epoch.Unix())+float64(epoch.Nanosecond())/1e9, *clockWindow)
		fmt.Printf("Conservative time/region screening retained %d track segments; no fixed aircraft identity\n", len(ts))
	}
	if len(events) == 0 && len(reviewIntervals) > 0 {
		var spans []acoustic.Event
		for _, w := range reviewIntervals {
			spans = append(spans, acoustic.Event{Start: w.Start, End: w.End})
		}
		epoch, e := time.Parse(time.RFC3339Nano, start)
		fail(e)
		ts = trimRegionTracks(ts, bounds, spans, float64(epoch.Unix())+float64(epoch.Nanosecond())/1e9, *clockWindow)
		fmt.Printf("Conservative time/region screening retained %d track segments\n", len(ts))
	}
	var corridorEvents []acoustic.Event
	for _, e := range events {
		corridorEvents = append(corridorEvents, acoustic.Event{Start: e.Start, End: e.End, MinSpeed: 0, MaxSpeed: 1000, MaxSlant: 15000})
	}
	if len(events) == 0 {
		for _, w := range reviewIntervals {
			corridorEvents = append(corridorEvents, acoustic.Event{Start: w.Start, End: w.End, MinSpeed: 0, MaxSpeed: 1000, MaxSlant: 15000})
		}
	}
	if *corridors && len(corridorEvents) == 0 {
		panic("--corridors requires observed intervals or curves")
	}
	var smoothing []tracks.SmoothingReport
	var reconstruction []tracks.ReconstructionReport
	if *smoothStep > 0 {
		for i := range ts {
			if *reconstruct {
				estimate, report, err := ts[i].Reconstruct(tracks.DefaultReconstructionOptions())
				fail(err)
				ts[i] = estimate
				reconstruction = append(reconstruction, report)
			}
			smoothed, report, e := ts[i].Smooth(tracks.SmoothingOptions{Step: *smoothStep, MaxGap: 30, MaxAcceleration: 20, UseKinematics: *reportedMotion})
			fail(e)
			ts[i] = smoothed
			smoothing = append(smoothing, report)
		}
	}
	if *reconstruct {
		nodes, rejected, shift := 0, 0, 0.
		for _, r := range reconstruction {
			nodes += r.Nodes
			rejected += r.Rejected
			shift = math.Max(shift, r.MaxShift)
		}
		fmt.Printf("Telemetry reconstruction: %d nodes, %d chunks retained original, maximum correction %.2f m; fixed options, no audio/receiver used\n", nodes, rejected, shift)
	}
	if *reportedMotion {
		n := 0
		for _, r := range smoothing {
			n += r.ReportedHorizontal
		}
		if n == 0 {
			panic("no reported horizontal motion in supplied tracks")
		}
	}
	for i := range ts {
		ts[i] = ts[i].PrepareSegments()
	}
	nearby := func(geo.Vec) []tracks.Track { return ts }
	if *indexed {
		nearby = regionTrackIndex(ts, 15000)
	}
	terrain := geo.Terrain{Dir: cfg.Terrain}
	geoid, e := geo.LoadGeoid(cfg.Geoid)
	fail(e)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	ctx, timeout := context.WithTimeout(ctx, 90*time.Minute)
	defer timeout()
	fail(os.MkdirAll(*out, 0700))
	o := search.DefaultGuidedOptions()
	o.StepHz = *sourceStep
	o.MaxHz = math.Min(6000, 2*s.Hz(len(s.Rows[0])-1))
	o.Workers = 1
	o.TrainingOnly = *deferValidation
	origin := 0.
	o.FoldOrigin = &origin
	if *reviewWindows != "" {
		b, e := os.ReadFile(*reviewWindows)
		fail(e)
		fail(json.Unmarshal(b, &o.ReviewIntervals))
	}
	seen := map[[2]int64]bool{}
	var all []regionPoint
	makeGrid := func(b geo.Bounds, spacing float64) []regionPoint {
		coarseCorridors := *corridors && spacing == *grid && b == bounds
		points, e := search.GridLocations(ctx, search.Config{Bounds: b, RegionMask: *state, Grid: spacing, MaxGridPoints: *maxPoints, Corridors: coarseCorridors, Events: corridorEvents, RecordingStart: start, ClockUncertainty: *clockWindow, MaxGap: 30, SoundSpeed: 343}, ts)
		fail(e)
		var result []regionPoint
		for _, q := range points {
			if !bounds.Contains(q[0], q[1]) {
				continue
			}
			key := [2]int64{int64(math.Round(q[0] * 1e8)), int64(math.Round(q[1] * 1e8))}
			if seen[key] {
				continue
			}
			seen[key] = true
			if len(seen) > *maxPoints {
				panic("regional scan exceeds max-points; bounds/spacing unchanged")
			}
			h, e := terrain.Height(q[0], q[1])
			if e != nil && os.IsNotExist(e) && *downloadTerrain {
				name := geo.TileName(q[0], q[1])
				fmt.Println("Downloading missing terrain", name)
				fail(geo.DownloadTerrain(ctx, cfg.Terrain, name))
				h, e = terrain.Height(q[0], q[1])
			}
			fail(e)
			n, e := geoid.Height(q[0], q[1])
			fail(e)
			result = append(result, regionPoint{Lat: q[0], Lon: q[1], Height: h + n + 1.5, Spacing: spacing, Matches: []search.GuidedMatch{}})
		}
		return result
	}
	evaluate := func(batch []regionPoint) {
		jobs := make(chan int)
		done := make(chan regionPoint, *workers)
		var wg sync.WaitGroup
		for w := 0; w < *workers; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for index := range jobs {
					point := batch[index]
					localTracks := nearby(geo.ECEF(point.Lat, point.Lon, point.Height))
					for clock := -*clockWindow; clock <= *clockWindow+1e-9; clock += *clockStep {
						h := search.ReceiverHypothesis{Lat: point.Lat, Lon: point.Lon, Height: point.Height, RecordingStart: start, Clock: clock, SoundSpeed: 343, MaxGap: 30}
						if len(events) > 0 {
							r, e := search.FitJointRidges(ctx, search.CurveReceiver{ReceiverHypothesis: h}, localTracks, events, search.CurveModelOptions{Window: float64(s.Window) / float64(s.SampleRate), MaxSourceChange: *maxSourceChange})
							fail(e)
							if r.Valid && (!point.Valid || -r.WorstTraining > point.Rank) {
								point.Clock = clock
								point.Rank = -r.WorstTraining
								point.Metric = "negative worst normalized full-curve training RMSE; reserved checks are descriptive ridge shape only"
								point.Valid = true
								point.Pass = r.ReservedPass
								point.Ridges = r.Matches
								point.Validation = "descriptive_ridge_shape"
							}
							continue
						}
						r, e := search.ReviewReceiver(ctx, h, s, localTracks, o)
						fail(e)
						chosen := []search.GuidedMatch{}
						score := math.Inf(1)
						pass := true
						for _, m := range r.Matches {
							if !m.Fit.Valid {
								continue
							}
							conflict := false
							for _, p := range chosen {
								if identity(m.Track) == identity(p.Track) || m.Interval > 0 && m.Interval == p.Interval || m.Start < p.End+1 && m.End > p.Start-1 {
									conflict = true
									break
								}
							}
							if conflict {
								continue
							}
							score = math.Min(score, m.Fit.Selection)
							pass = pass && m.Supported
							m.Curve = nil
							m.Controls = nil
							m.Alternatives = nil
							chosen = append(chosen, m)
							if len(chosen) == *minimum {
								break
							}
						}
						if len(chosen) == *minimum && (!point.Valid || score > point.Score) {
							point.Clock = clock
							point.Score = score
							point.Rank = score
							point.Metric = "weakest training spectral selection (dB)"
							point.Valid = true
							point.Pass = pass
							point.Matches = chosen
							point.Validation = "local_checks_evaluated"
							if *deferValidation {
								point.Validation = "not_evaluated_training_only"
							}
						}
					}
					done <- point
				}
			}()
		}
		go func() {
			defer close(jobs)
			for i := range batch {
				select {
				case jobs <- i:
				case <-ctx.Done():
					return
				}
			}
		}()
		go func() { wg.Wait(); close(done) }()
		n := 0
		for point := range done {
			all = append(all, point)
			n++
			if n == 1 || n%50 == 0 || n == len(batch) {
				fmt.Printf("Scored %d/%d in grid stage; %d total points\n", n, len(batch), len(all))
			}
		}
		fail(ctx.Err())
	}
	peaks := func(separation float64, count int) []regionPoint {
		ordered := append([]regionPoint(nil), all...)
		sort.SliceStable(ordered, func(i, j int) bool {
			if ordered[i].Valid != ordered[j].Valid {
				return ordered[i].Valid
			}
			if ordered[i].Rank != ordered[j].Rank {
				return ordered[i].Rank > ordered[j].Rank
			}
			if ordered[i].Lat != ordered[j].Lat {
				return ordered[i].Lat < ordered[j].Lat
			}
			return ordered[i].Lon < ordered[j].Lon
		})
		var selected []regionPoint
		if *diverse && separation > 0 {
			seenGroups := map[string]bool{}
			for _, p := range ordered {
				if !p.Valid {
					continue
				}
				ids := []string{}
				for _, r := range p.Ridges {
					ids = append(ids, r.Aircraft)
				}
				for _, m := range p.Matches {
					ids = append(ids, identity(m.Track))
				}
				group := strings.Join(ids, "/")
				if seenGroups[group] {
					continue
				}
				keep := true
				for _, q := range selected {
					if geo.ECEF(p.Lat, p.Lon, 0).Sub(geo.ECEF(q.Lat, q.Lon, 0)).Norm() < separation {
						keep = false
						break
					}
				}
				if keep {
					selected = append(selected, p)
					seenGroups[group] = true
					if len(selected) == count {
						return selected
					}
				}
			}
		}
		for _, p := range ordered {
			if !p.Valid {
				continue
			}
			keep := true
			for _, q := range selected {
				if geo.ECEF(p.Lat, p.Lon, 0).Sub(geo.ECEF(q.Lat, q.Lon, 0)).Norm() < separation {
					keep = false
					break
				}
			}
			if keep {
				selected = append(selected, p)
				if len(selected) == count {
					break
				}
			}
		}
		return selected
	}
	evaluate(makeGrid(bounds, *grid))
	for _, stage := range []struct {
		spacing, radius, separation float64
		count                       int
	}{{*refine, *grid, 2 * *grid, *refinePeaks}, {*final, *refine, 2 * *refine, *finalPeaks}} {
		var batch []regionPoint
		for _, p := range peaks(stage.separation, stage.count) {
			dy := stage.radius / 111320
			dx := dy / math.Cos(p.Lat*math.Pi/180)
			b := geo.Bounds{South: math.Max(bounds.South, p.Lat-dy), North: math.Min(bounds.North, p.Lat+dy), West: math.Max(bounds.West, p.Lon-dx), East: math.Min(bounds.East, p.Lon+dx)}
			if b.Valid() {
				batch = append(batch, makeGrid(b, stage.spacing)...)
			}
		}
		evaluate(batch)
	}
	best := peaks(0, 1)
	ranked := peaks(300, 8)
	if *deferValidation && len(events) == 0 {
		confirmedOptions := o
		confirmedOptions.TrainingOnly = false
		for i := range ranked {
			p := &ranked[i]
			h := search.ReceiverHypothesis{Lat: p.Lat, Lon: p.Lon, Height: p.Height, RecordingStart: start, Clock: p.Clock, SoundSpeed: 343, MaxGap: 30}
			r, err := search.ReviewReceiver(ctx, h, s, nearby(geo.ECEF(p.Lat, p.Lon, p.Height)), confirmedOptions)
			fail(err)
			p.Pass = true
			for j, old := range p.Matches {
				found := false
				for _, m := range r.Matches {
					if m.Track == old.Track && m.Interval == old.Interval && m.Start == old.Start && m.End == old.End && m.Fit.F0 == old.Fit.F0 {
						found = true
						p.Pass = p.Pass && m.Supported
						ridgePreview(s, filepath.Join(*out, fmt.Sprintf("peak-%02d-curve-%02d.svg", i+1, j+1)), acoustic.Event{Start: m.Start, End: m.End}, m.Curve)
						m.Curve = nil
						m.Controls = nil
						m.Alternatives = nil
						p.Matches[j] = m
						break
					}
				}
				if !found {
					panic("confirmation changed frozen training-selected source/window/aircraft")
				}
			}
			p.Validation = "frozen_training_winner_checked"
			writeJSON(filepath.Join(*out, fmt.Sprintf("validation-peak-%02d.json", i+1)), r)
			for j := range all {
				if all[j].Lat == p.Lat && all[j].Lon == p.Lon {
					all[j] = *p
				}
			}
			if len(best) > 0 && best[0].Lat == p.Lat && best[0].Lon == p.Lon {
				best[0] = *p
			}
		}
	}
	if len(events) > 0 {
		for i, p := range ranked[:min(3, len(ranked))] {
			for _, m := range p.Ridges {
				for _, t := range ts {
					if t.ID != m.Track {
						continue
					}
					h := search.CurveReceiver{ReceiverHypothesis: search.ReceiverHypothesis{Lat: p.Lat, Lon: p.Lon, Height: p.Height, RecordingStart: start, Clock: p.Clock, SoundSpeed: 343, MaxGap: 30}}
					e := events[m.Event-1]
					window := math.Max(e.Resolution, float64(s.Window)/float64(s.SampleRate))
					r, err := search.FitRidgeCurve(ctx, h, t, e, search.CurveModelOptions{Window: window, MaxSourceChange: *maxSourceChange})
					fail(err)
					for _, f := range r.Fits {
						if f.Selected {
							ridgePreview(s, filepath.Join(*out, fmt.Sprintf("peak-%02d-curve-%02d.svg", i+1, m.Event)), e, f.Curve)
						}
					}
				}
			}
		}
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].Lat != all[j].Lat {
			return all[i].Lat < all[j].Lat
		}
		return all[i].Lon < all[j].Lon
	})
	metadata := map[string]any{"measured_ridge_harmonics": *harmonicSamples, "ridge_measurements": measurements, "polish_ridges": *polish, "observed_curves": events, "ridge_events": *ridgePath, "reported_motion": *reportedMotion, "max_source_change": *maxSourceChange, "recording": *record, "audio": audio, "spectrum": *spectrum, "tracks": trackPath, "bounds": bounds, "reference_coordinates_used_for_fitting": false, "source_frequency_step_hz": o.StepHz, "smooth_track_step_s": *smoothStep, "smoothing": smoothing, "fixed_fold_origin_audio_s": *o.FoldOrigin, "minimum_distinct_flights": *minimum, "clock_window_s": *clockWindow, "clock_step_s": *clockStep, "grid_m": *grid, "refine_m": *refine, "final_grid_m": *final, "points": all, "training_winner": best, "separated_training_peaks": ranked, "started_utc": started.UTC(), "elapsed_s": time.Since(started).Seconds(), "created_utc": time.Now().UTC(), "note": "Regional experimental matched-filter scan, not a blind state-wide result or verified GPS. Bounds are a disclosed spatial prior. A supplied ridge-events file restricts the study to complete audio-selected measured curves. No fixed flight identity is supplied. Ridge-mode ranking minimizes the worst normalized training residual with distinct aircraft and one shared clock; its reserved checks describe ridge shape only, not independent spectral validation. Raw mode ranks spectral evidence. Training evidence alone selects receiver, shared clock, source tone, window and distinct nonoverlapping flight assignments. The train/reserved partition is fixed across locations and incorrect-time trials. Ridge mode fixes it per observed curve; raw mode uses a fixed recording-wide origin. Reserved checks do not rescue a failed winner. Grid spacing is not location accuracy. Microphone motion, look-elsewhere effects and full-search false-positive calibration remain unresolved; all points and competing peaks are preserved."}
	metadata["reconstruct_track"] = *reconstruct
	metadata["trajectory_reconstruction"] = reconstruction
	if len(events) > 0 {
		delete(metadata, "fixed_fold_origin_audio_s")
		metadata["fold_partition"] = "fixed per observed curve across all receiver and incorrect-time hypotheses"
	} else {
		metadata["fold_partition"] = "fixed recording-wide origin"
	}
	metadata["state_mask"] = *state
	metadata["coarse_flight_corridors"] = *corridors
	metadata["spatial_index"] = *indexed
	metadata["refine_peaks"] = *refinePeaks
	metadata["final_peaks"] = *finalPeaks
	metadata["diverse_flight_seeds"] = *diverse
	if *state != "" {
		metadata["search_scope"] = "Full supplied state polygon; all time-eligible supplied aircraft corridors, with no known receiver coordinate or city bounds"
	}
	metadata["evaluated_points"] = len(all)
	metadata["confirmation_deferred_to_frozen_candidates"] = *deferValidation
	plotPoints := all
	if *compressPoints {
		f, e := os.Create(filepath.Join(*out, "points.jsonl.gz"))
		fail(e)
		z, e := gzip.NewWriterLevel(f, gzip.BestSpeed)
		fail(e)
		encoder := json.NewEncoder(z)
		for _, p := range all {
			fail(encoder.Encode(p))
		}
		fail(z.Close())
		fail(f.Close())
		plotPoints = displayRegionPoints(all, ranked, *mapLimit, *grid)
		metadata["points"] = plotPoints
		metadata["all_points_file"] = "points.jsonl.gz"
		metadata["points_scope"] = "Map display sample only; every evaluated point is retained in all_points_file"
	}
	metadata["map_points"] = len(plotPoints)
	writeJSON(filepath.Join(*out, "scan.json"), metadata)
	data, e := json.Marshal(plotPoints)
	fail(e)
	for _, name := range []string{"leaflet.js", "leaflet.css"} {
		b, e := os.ReadFile(filepath.Join("internal/mapview/assets", name))
		fail(e)
		fail(os.WriteFile(filepath.Join(*out, name), b, 0600))
	}
	markup := `<!doctype html><meta charset="utf-8"><title>Regional matched-filter study</title><link rel="stylesheet" href="leaflet.css"><style>body{margin:0;background:#15252b;color:#e2eeee;font:16px system-ui}header{padding:16px}a{color:#8fd9cc}#map{height:78vh}</style><header><h1>Regional matched-filter study</h1><p>Training-ranked candidate fits; no verified GPS. Bounds are a supplied prior. 10 m grid spacing does not establish 10 m accuracy. Click a point for its checks.</p><a href="scan.json">All candidates and limitations</a></header><div id="map"></div><script src="leaflet.js"></script><script>const points=__DATA__;const map=L.map('map',{preferCanvas:true});L.tileLayer('https://tile.openstreetmap.org/{z}/{x}/{y}.png',{maxZoom:19,attribution:'© OpenStreetMap contributors'}).addTo(map);const valid=points.filter(p=>p.model_available);const low=Math.min(...valid.map(p=>p.training_rank)),high=Math.max(...valid.map(p=>p.training_rank));for(const p of points){const f=p.model_available?(p.training_rank-low)/(high-low||1):0;const color=p.model_available?'hsl('+(250-250*f)+',85%,55%)':'#888';L.circleMarker([p.lat,p.lon],{radius:p.grid_m<=10?3:4,color,fillColor:color,fillOpacity:.8,weight:.4}).addTo(map).bindPopup(p.lat.toFixed(6)+', '+p.lon.toFixed(6)+'<br>Training score '+p.training_rank.toFixed(2)+'<br>'+p.ranking_metric+'<br>Reserved checks '+p.all_reserved_checks_pass+'<br>Shared clock '+p.shared_clock_correction_s+' s<br>Grid '+p.grid_m+' m<br>'+(p.ridge_matches||p.training_selected_matches).map(m=>m.track_id+'; source '+(m.fit.source_hz||m.fit.f0_hz)+' Hz').join('<br>'));}if(points.length)map.fitBounds(L.latLngBounds(points.map(p=>[p.lat,p.lon])));</script>`
	fail(os.WriteFile(filepath.Join(*out, "index.html"), []byte(strings.Replace(markup, "__DATA__", string(data), 1)), 0600))
	if len(best) > 0 {
		fmt.Printf("Training winner %.6f, %.6f, clock %+.1f s; reserved checks %v. Not verified GPS.\n", best[0].Lat, best[0].Lon, best[0].Clock, best[0].Pass)
	}
}
