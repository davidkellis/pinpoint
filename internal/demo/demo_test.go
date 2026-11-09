package demo

import (
	"context"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"pinpoint/internal/acoustic"
	"pinpoint/internal/geo"
	"pinpoint/internal/search"
	"pinpoint/internal/tracks"
)

func TestSyntheticAudioGeolocation(t *testing.T) {
	if testing.Short() {
		t.Skip("end-to-end audio test")
	}
	if _, e := exec.LookPath("ffmpeg"); e != nil {
		t.Skip("ffmpeg unavailable")
	}
	dir := t.TempDir()
	c, e := Generate(dir)
	if e != nil {
		t.Fatal(e)
	}
	s, e := acoustic.Analyze(context.Background(), filepath.Join(dir, "audio.wav"), acoustic.Defaults())
	if e != nil {
		t.Fatal(e)
	}
	f, e := os.Open(filepath.Join(dir, "tracks.csv"))
	if e != nil {
		t.Fatal(e)
	}
	ts, e := tracks.Read(f)
	f.Close()
	if e != nil {
		t.Fatal(e)
	}
	c.Workers = 2
	c.Bounds = geo.Bounds{South: Lat - 0.003, North: Lat + 0.003, West: Lon - 0.004, East: Lon + 0.004}
	c.Grid = 200
	c.Refine = 40
	r, e := search.Run(context.Background(), c, s, ts)
	if e != nil {
		t.Fatal(e)
	}
	if len(r.Candidates) == 0 {
		t.Fatal("no match for synthetic flyovers")
	}
	best := r.Candidates[0]
	if len(r.ScoreGrid) != r.Evaluated || len(r.ScoreGrid) <= len(r.Candidates) {
		t.Fatal("map grid lost evaluated locations")
	}
	var mappedBest bool
	for _, p := range r.ScoreGrid {
		if math.Abs(p.Lat-best.Lat) < 1e-8 && math.Abs(p.Lon-best.Lon) < 1e-8 && p.MeetsThreshold && p.Score != nil && math.Abs(*p.Score-best.Score) < 1e-8 {
			mappedBest = true
		}
	}
	if !mappedBest {
		t.Fatal("best location is missing from the map grid")
	}
	distance := geo.ECEF(best.Lat, best.Lon, best.Height).Sub(geo.ECEF(Lat, Lon, Height)).Norm()
	if distance > 80 || math.Abs(best.ClockOffset-2) > 0.5 || best.Events[0].Track != "synthetic-A" || best.Events[1].Track != "synthetic-B" {
		t.Fatalf("wrong location (%vm error), timing or flights: %+v", distance, best)
	}
	t.Logf("synthetic recovery error %.1fm; clock offset %.2fs", distance, best.ClockOffset)

	t.Run("automatic detection retains both aircraft through rapid Doppler change", func(t *testing.T) {
		options := acoustic.Defaults()
		options.Window = 16384
		options.Hop = 2048
		webSpectrum, err := acoustic.Analyze(context.Background(), filepath.Join(dir, "audio.wav"), options)
		if err != nil {
			t.Fatal(err)
		}
		one := c
		one.Bounds = geo.Bounds{South: Lat - .00001, North: Lat + .00001, West: Lon - .00001, East: Lon + .00001}
		one.Refine = 0
		found := map[string]bool{}
		for _, event := range webSpectrum.Discover() {
			one.Events = []acoustic.Event{event}
			report, err := search.Run(context.Background(), one, webSpectrum, ts)
			if err != nil {
				t.Fatal(err)
			}
			for _, candidate := range report.Candidates {
				found[candidate.Events[0].Track] = true
			}
		}
		if !found["synthetic-A"] || !found["synthetic-B"] {
			t.Fatalf("missed a known aircraft in automatic analysis: %v", found)
		}
	})
	// Use a very small region for negative controls to keep the suite inexpensive.
	c.Bounds = geo.Bounds{South: Lat - 0.00001, North: Lat + 0.00001, West: Lon - 0.00001, East: Lon + 0.00001}
	c.Refine = 0
	// Each shifted event can fit its own clock, but both cannot fit one clock.
	c.ClockUncertainty = 25
	c.ClockStep = 1
	c.MinScore = 20
	shifted := *s
	shifted.Rows = make([][]float32, len(s.Rows))
	for i, row := range s.Rows {
		shifted.Rows[i] = append([]float32{}, row...)
	}
	shift := int(20 / s.Step())
	for i := range s.Rows {
		if s.Time(i) >= 110 {
			for j := range shifted.Rows[i] {
				shifted.Rows[i][j] = 0
			}
			if i >= shift {
				copy(shifted.Rows[i], s.Rows[i-shift])
			}
		}
	}
	second := c.Events[1]
	second.Start += 20
	second.End = 217
	second.CPATime += 20
	c.Events[1] = second
	for _, event := range c.Events {
		single := c
		single.Events = []acoustic.Event{event}
		r, e = search.Run(context.Background(), single, &shifted, ts)
		if e != nil || len(r.Candidates) == 0 {
			t.Fatalf("single event should fit independently: %v %+v", e, r)
		}
	}
	r, e = search.Run(context.Background(), c, &shifted, ts)
	if e != nil {
		t.Fatal(e)
	}
	if r.Status != "no_match" {
		t.Fatal("accepted inconsistent recording clocks")
	}
	// A recording without tonal evidence must not produce a fabricated location.
	for _, row := range s.Rows {
		clear(row)
	}
	c.MinScore = 5
	c.ClockUncertainty = 0
	r, e = search.Run(context.Background(), c, s, ts)
	if e != nil {
		t.Fatal(e)
	}
	if r.Status != "no_match" || len(r.Candidates) > 0 {
		t.Fatal("silence produced a location")
	}
}
