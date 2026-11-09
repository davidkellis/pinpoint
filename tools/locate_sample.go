//go:build ignore

// Conditional single-passage evaluation. Source CSV supplies an inferred clock;
// automatic audio evidence chooses the curve. No receiver coordinate is input.
package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"encoding/xml"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"time"

	"pinpoint/internal/acoustic"
	"pinpoint/internal/geo"
	"pinpoint/internal/search"
	"pinpoint/internal/tracks"
)

func main() {
	analysis := flag.String("analysis", "", "fresh analysis directory")
	labels := flag.String("csv", "", "publisher UTC frequency measurements, used for inferred clock only")
	kml := flag.String("kml", "", "supplied known-aircraft trajectory")
	id := flag.String("track-id", "", "supplied aircraft registration")
	competing := flag.String("tracks", "", "independent competing California trajectory CSV, instead of the supplied track")
	out := flag.String("out", "", "new results directory")
	workers := flag.Int("workers", 12, "bounded scoring workers")
	state := flag.String("state", "CA", "state search mask")
	clock := flag.String("recording-start", "", "known approximate start, RFC3339; otherwise infer from publisher CSV")
	uncertainty := flag.Float64("clock-uncertainty", 5, "recording start uncertainty, seconds")
	method := flag.String("method", "single", "single known passage or automatic multi-flyover groups")
	audio := flag.String("audio", "", "original recording for automatic reserved-audio validation")
	flag.Parse()
	if *analysis == "" || *out == "" || *clock == "" && *labels == "" || *competing == "" && (*kml == "" || *id == "") || *method != "single" && *method != "automatic" {
		panic("supply --analysis --out, clock or --csv, and --tracks or a supplied --kml/--track-id")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	must(os.MkdirAll(*out, 0755))
	s, err := acoustic.Load(filepath.Join(*analysis, "spectrogram.bin"))
	must(err)
	var detection acoustic.DetectionReport
	b, err := os.ReadFile(filepath.Join(*analysis, "detection.json"))
	must(err)
	must(json.Unmarshal(b, &detection))
	events := acoustic.SearchEvents(detection.Events, true, false)
	if len(events) == 0 || *method == "automatic" && len(events) < 2 {
		must(search.JSON(filepath.Join(*out, "outcome.json"), map[string]any{"status": "no_usable_curve", "note": "No automatically strong predictive curve; no location was inferred."}))
		return
	}
	if *method == "single" {
		sort.SliceStable(events, func(i, j int) bool { return events[i].End-events[i].Start > events[j].End-events[j].Start })
		events = events[:1]
		events[0].Partial, events[0].CPATolerance, events[0].CPATime = true, 0, (events[0].Start+events[0].End)/2
	}
	var start time.Time
	var alignment map[string]any
	if *clock != "" {
		start, err = time.Parse(time.RFC3339Nano, *clock)
		must(err)
		alignment = map[string]any{"method": "user-confirmed filename start time and time zone; no GPS or aircraft identity supplied", "recording_start": start}
	} else {
		start, alignment = alignClock(s, *labels)
	}
	must(search.JSON(filepath.Join(*out, "alignment.json"), alignment))
	g, err := geo.LoadGeoid("work/web/cache/egm96_15.gtx")
	must(err)
	var ts []tracks.Track
	mode := "supplied-aircraft conditional fit; no competing aircraft"
	trackPath := *competing
	if trackPath != "" {
		f, err := os.Open(trackPath)
		must(err)
		ts, err = tracks.Read(f)
		f.Close()
		must(err)
		mode = *state + " search against independently imported competing aircraft"
	} else {
		tr := readKML(*kml, *id, g)
		ts = []tracks.Track{tr}
		trackPath = filepath.Join(*out, "supplied-target.csv")
		writeTrack(trackPath, tr)
	}
	c := search.DefaultConfig()
	c.Spectrum, _ = filepath.Abs(filepath.Join(*analysis, "spectrogram.bin"))
	c.Tracks, _ = filepath.Abs(trackPath)
	c.Bounds, _ = geo.StateBounds(*state)
	c.RegionMask = *state
	c.RecordingStart = start.UTC().Format(time.RFC3339Nano)
	c.ClockUncertainty, c.ClockStep = *uncertainty, .5
	if *method == "automatic" {
		c.ClockStep = .25
	}
	c.Corridors, c.OmitUnmodeled = true, true
	c.Grid, c.Refine, c.FinalGrid = 200, 50, 10
	c.MaxGridPoints, c.Workers = 2000000, *workers
	c.TerrainDir, _ = filepath.Abs("work/web/cache/terrain")
	if *state == "CA" {
		c.TerrainDir, _ = filepath.Abs("data/terrain-ca-3arc")
	}
	c.GeoidGrid, _ = filepath.Abs("work/web/cache/egm96_15.gtx")
	c.Events = events
	must(search.JSON(filepath.Join(*out, "search.json"), c))
	terrain := c
	if *method == "automatic" {
		for _, shift := range search.DefaultSequenceOptions(c).ControlShifts {
			terrain.ClockUncertainty = math.Max(terrain.ClockUncertainty, c.ClockUncertainty+math.Abs(shift))
		}
	}
	tiles, err := search.TerrainTiles(ctx, terrain, ts)
	must(err)
	jobs := make(chan string)
	errors := make(chan error, len(tiles))
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for tile := range jobs {
				errors <- geo.DownloadTerrain(ctx, c.TerrainDir, tile)
			}
		}()
	}
	go func() {
		defer close(jobs)
		for _, tile := range tiles {
			jobs <- tile
		}
	}()
	wg.Wait()
	close(errors)
	for err := range errors {
		must(err)
	}
	if *method == "automatic" {
		o := search.DefaultSequenceOptions(c)
		o.AudioPath = *audio
		if o.AudioPath == "" {
			o.AudioPath = s.Source
		}
		o.CacheDir = filepath.Join(*out, "sequence-cache")
		last := time.Time{}
		progress := func(p search.SequenceProgress) {
			if p.Stage == "starting" || p.Stage == "finished" || time.Since(last) > 10*time.Second {
				fmt.Printf("Trial %d/%d · %d events · group %d/%d · %s %d/%d\n", p.Trial, p.Trials, p.Size, p.Group, p.Groups, p.Stage, p.Done, p.Total)
				last = time.Now()
			}
		}
		completed := func(g search.SequenceGroup, q search.Config, r search.Report) error {
			if g.Trial != 0 || g.Best == nil {
				return nil
			}
			return search.WriteReport(filepath.Join(*out, fmt.Sprintf("group-%03d", g.ID)), r, q, s, ts)
		}
		// Failed necessary criteria cannot become a positive by running nulls.
		// Keep this evaluation shortcut explicit; production defaults are unchanged.
		actual := o
		actual.ControlShifts = nil
		r, runErr := search.RunAutomaticSequences(ctx, c, s, ts, actual, progress, completed)
		if runErr == nil && r.Status != "incomplete" && r.Selected != nil && r.Groups[*r.Selected].Pass {
			fmt.Println("Actual-time group passes reserved checks; running all eight full wrong-time searches")
			r, runErr = search.RunAutomaticSequences(ctx, c, s, ts, o, progress, completed)
		} else if runErr == nil && r.Status != "incomplete" {
			r.Note += " Evaluation staging: wrong-time controls were not run because no actual-time training group exists or the selected group already fails mandatory reserved-audio checks. No positive location is asserted; nulls cannot repair these failed necessary criteria. Production eight-control defaults are unchanged."
			if r.Selected != nil {
				r.Status = "unresolved"
				r.Reasons = append(r.Reasons, search.Rejection{Code: "reserved_audio_failure", Detail: "The training-selected group fails reserved-audio validation. Wrong-time controls cannot turn that failure into a positive result and were not run."})
			}
		}
		must(search.WriteSequenceReport(*out, r, func(id int) string { return fmt.Sprintf("group-%03d/map.html", id) }))
		must(search.JSON(filepath.Join(*out, "outcome.json"), map[string]any{"status": r.Status, "mode": mode, "group_count": len(r.Groups), "selected_group_id": r.Selected, "rejections": r.Reasons, "error": r.Error, "note": r.Note}))
		must(runErr)
		return
	}
	last := time.Time{}
	r, err := search.RunTiled(ctx, c, s, ts, func(stage string, done, total int) {
		if time.Since(last) > 10*time.Second {
			fmt.Printf("%s %d/%d\n", stage, done, total)
			last = time.Now()
		}
	})
	must(err)
	r.Note += " Evaluation scope: " + mode + ". The longest automatically usable curve is selected because the source clip contains one passage. Exact microphone GPS is unpublished. No receiver coordinate is supplied to scoring. A model match is not independently verified localization."
	must(search.WriteReport(*out, r, c, s, ts))
	must(search.JSON(filepath.Join(*out, "outcome.json"), map[string]any{"status": r.Status, "mode": mode, "recording_start_inferred": c.RecordingStart, "locations_evaluated": r.Evaluated, "ambiguity": r.Ambiguity, "candidates": r.Candidates, "flight_groups": r.FlightGroups, "event": events[0], "note": r.Note}))
	fmt.Println(r.Status, r.Evaluated)
}

func alignClock(s *acoustic.Spectrum, path string) (time.Time, map[string]any) {
	f, err := os.Open(path)
	must(err)
	rows, err := csv.NewReader(f).ReadAll()
	f.Close()
	must(err)
	var first time.Time
	var points []acoustic.RidgeSample
	for i, row := range rows[1:] {
		at, err := time.Parse(time.RFC3339Nano, row[0])
		must(err)
		hz, err := strconv.ParseFloat(row[1], 64)
		must(err)
		if i == 0 {
			first = at
		}
		points = append(points, acoustic.RidgeSample{Time: at.Sub(first).Seconds(), Hz: hz})
	}
	if len(points) < 5 {
		panic("too few publisher clock anchors")
	}
	best, offset, available := math.Inf(-1), 0., false
	for t := s.Time(0); t <= s.Time(len(s.Rows)-1)-points[len(points)-1].Time; t += .025 {
		sum, valid := 0., true
		for _, p := range points {
			x := (t + p.Time - s.Time(0)) / s.Step()
			i := int(math.Floor(x))
			a, ok := s.Value(i, p.Hz)
			b, yes := s.Value(i+1, p.Hz)
			if !ok || !yes {
				valid = false
				break
			}
			sum += a + (x-float64(i))*(b-a)
		}
		if valid && sum/float64(len(points)) > best {
			best, offset, available = sum/float64(len(points)), t, true
		}
	}
	if !available {
		panic("publisher trace cannot be aligned inside the recording")
	}
	start := first.Add(-time.Duration(math.Round(offset * 1e9)))
	return start, map[string]any{"method": "Audio-only 0.025 s alignment sweep along publisher frequency measurements. No flight geometry or receiver location enters clock alignment.", "publisher_csv": path, "first_csv_utc": first, "first_csv_offset_in_audio_s": offset, "inferred_wav_start_utc": start, "mean_spectral_contrast_db": best, "clock_is_independently_verified": false}
}

func readKML(path, id string, g *geo.Geoid) tracks.Track {
	f, err := os.Open(path)
	must(err)
	defer f.Close()
	d := xml.NewDecoder(f)
	var times, coordinates []string
	for {
		t, err := d.Token()
		if err == io.EOF {
			break
		}
		must(err)
		if e, ok := t.(xml.StartElement); ok && (e.Name.Local == "when" || e.Name.Local == "coord") {
			var v string
			must(d.DecodeElement(&v, &e))
			if e.Name.Local == "when" {
				times = append(times, v)
			} else {
				coordinates = append(coordinates, v)
			}
		}
	}
	if len(times) != len(coordinates) || len(times) < 2 {
		panic("invalid KML trajectory")
	}
	tr := tracks.Track{ID: id}
	seen := map[float64]bool{}
	for i, text := range coordinates {
		at, err := time.Parse(time.RFC3339Nano, times[i])
		must(err)
		epoch := float64(at.Unix()) + float64(at.Nanosecond())/1e9
		if seen[epoch] {
			continue
		}
		seen[epoch] = true
		var lon, lat, h float64
		_, err = fmt.Sscan(text, &lon, &lat, &h)
		must(err)
		undulation, err := g.Height(lat, lon)
		must(err)
		h += undulation
		tr.Points = append(tr.Points, tracks.Point{Time: epoch, Lat: lat, Lon: lon, Alt: h, Position: geo.ECEF(lat, lon, h)})
	}
	sort.Slice(tr.Points, func(i, j int) bool { return tr.Points[i].Time < tr.Points[j].Time })
	return tr
}

func writeTrack(path string, tr tracks.Track) {
	f, err := os.Create(path)
	must(err)
	w := csv.NewWriter(f)
	must(w.Write([]string{"track_id", "time_unix_s", "lat", "lon", "alt_ellipsoid_m", "type_code"}))
	for _, p := range tr.Points {
		must(w.Write([]string{tr.ID, fmt.Sprint(p.Time), fmt.Sprint(p.Lat), fmt.Sprint(p.Lon), fmt.Sprint(p.Alt), tr.TypeCode}))
	}
	w.Flush()
	must(w.Error())
	must(f.Close())
}
func must(err error) {
	if err != nil {
		panic(err)
	}
}
