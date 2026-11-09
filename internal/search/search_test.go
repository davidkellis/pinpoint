package search

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pinpoint/internal/acoustic"
	"pinpoint/internal/geo"
	"pinpoint/internal/tracks"
)

func scoreGridFixture() (Config, *acoustic.Spectrum, []tracks.Track) {
	const epoch = 1700000000.0
	c := DefaultConfig()
	c.RecordingStart = time.Unix(int64(epoch), 0).UTC().Format(time.RFC3339)
	c.ClockUncertainty = 0
	c.Refine = 0
	c.Workers = 2
	c.Bounds = geo.Bounds{South: -0.00001, North: 0.00001, West: -0.00001, East: 0.00001}
	h := 0.0
	c.ObserverHeight = &h
	c.Events = []acoustic.Event{{Start: 5.5, End: 35.5, CPATime: 23, FMin: 100, FMax: 100, FStep: 1, Harmonics: []int{1}, MinSpeed: 20, MaxSpeed: 160, MinSlant: 100, MaxSlant: 10000}}
	s := &acoustic.Spectrum{Version: 1, SampleRate: 1024, Window: 1024, Hop: 1024, FirstBin: 40}
	for i := 0; i < 41; i++ {
		s.Rows = append(s.Rows, make([]float32, 200))
	}
	track := tracks.Track{ID: "fixture"}
	observer := geo.ECEF(0, 0, 0)
	for i := 0; i <= 40; i += 2 {
		track.Points = append(track.Points, tracks.Point{Time: epoch + float64(i), Position: observer.Add(geo.Vec{X: 1000, Y: float64(i-20) * 70})})
	}
	return c, s, []tracks.Track{track}
}

func TestScoreGridPreservesWeakAndUnmodeledLocations(t *testing.T) {
	c, s, ts := scoreGridFixture()
	r, err := Run(context.Background(), c, s, ts)
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != "no_match" || len(r.Candidates) != 0 || len(r.ScoreGrid) != r.Evaluated || len(r.ScoreGrid) != 4 {
		t.Fatalf("incomplete grid or fabricated match: %+v", r)
	}
	for _, p := range r.ScoreGrid {
		if p.Score == nil || *p.Score != 0 || p.MeetsThreshold || len(p.EventScores) != 1 || p.Stage != "coarse" || p.Spacing != c.Grid {
			t.Fatalf("lost below-threshold evidence: %+v", p)
		}
	}
	// Telemetry that cannot explain this recording should be missing, not zero.
	for i := range ts[0].Points {
		ts[0].Points[i].Time += 1000
	}
	r, err = Run(context.Background(), c, s, ts)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range r.ScoreGrid {
		if p.Score != nil || p.ClockOffset != nil || p.MeetsThreshold || len(p.EventScores) != 0 {
			t.Fatalf("invented missing-data score: %+v", p)
		}
	}
	if _, err = json.Marshal(r); err != nil {
		t.Fatalf("nonfinite values leaked into report: %v", err)
	}
}

func TestScoreGridKeepsThresholdAndSharedFits(t *testing.T) {
	c, s, ts := scoreGridFixture()
	for _, row := range s.Rows {
		for i := range row {
			row[i] = 8
		}
	}
	r, err := Run(context.Background(), c, s, ts)
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != "candidate_matches" || len(r.Candidates) == 0 {
		t.Fatal("expected accepted score")
	}
	for _, p := range r.ScoreGrid {
		if p.Score == nil || math.Abs(*p.Score-8) > 1e-9 || !p.MeetsThreshold || p.ClockOffset == nil || *p.ClockOffset != 0 || p.TrackIDs[0] != "fixture" {
			t.Fatalf("grid differs from candidate fit: %+v", p)
		}
	}
}

func TestExplicitLargeSearchControls(t *testing.T) {
	c, s, ts := scoreGridFixture()
	c.MaxGridPoints = 3
	if _, err := Run(context.Background(), c, s, ts); err == nil {
		t.Fatal("ignored explicit grid limit")
	}
	c.MaxGridPoints = 2000001
	if _, err := c.Validate(s); err == nil {
		t.Fatal("accepted an unbounded grid budget")
	}
	c.MaxGridPoints = 4
	c.OmitUnmodeled = true
	for i := range ts[0].Points {
		ts[0].Points[i].Time += 1000
	}
	r, err := Run(context.Background(), c, s, ts)
	if err != nil || r.Evaluated != 4 || len(r.ScoreGrid) != 0 || r.Status != "no_match" {
		t.Fatalf("sparse output lost evaluated count or fabricated scores: %+v %v", r, err)
	}
}

func TestMapOutputContainsWholeGridAndHandlesNoMatch(t *testing.T) {
	c, s, _ := scoreGridFixture()
	s.Source = `recording </script><script>alert(1)</script>.wav`
	r := Report{Status: "no_match", Bounds: c.Bounds, Grid: c.Grid, MinScore: c.MinScore, Candidates: []Candidate{}, ScoreGrid: []ScorePoint{}}
	for i := 0; i < 25; i++ {
		score := float64(i) / 10
		r.ScoreGrid = append(r.ScoreGrid, ScorePoint{Lat: 0, Lon: float64(i) / 1e6, Score: &score, EventScores: []float64{score}, TrackIDs: []string{"track"}, Stage: "coarse", Spacing: 200})
	}
	r.ScoreGrid = append(r.ScoreGrid, ScorePoint{Lat: 0.001, Lon: 0, Stage: "coarse", Spacing: 200})
	r.Evaluated = len(r.ScoreGrid)
	dir := t.TempDir()
	if err := WriteReport(dir, r, c, s, nil); err != nil {
		t.Fatal(err)
	}
	bytes, err := os.ReadFile(filepath.Join(dir, "score-grid.geojson"))
	if err != nil {
		t.Fatal(err)
	}
	var grid struct {
		Features []struct {
			Geometry   struct{ Coordinates [2]float64 }
			Properties ScorePoint
		}
	}
	if err = json.Unmarshal(bytes, &grid); err != nil {
		t.Fatal(err)
	}
	if len(grid.Features) != 26 {
		t.Fatal("grid was truncated to ranked candidates")
	}
	if grid.Features[25].Properties.Score != nil {
		t.Fatal("missing score was serialized as zero")
	}
	if grid.Features[1].Geometry.Coordinates != [2]float64{1e-6, 0} {
		t.Fatal("GeoJSON longitude/latitude reversed")
	}
	page, err := os.ReadFile(filepath.Join(dir, "map.html"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(page), s.Source) || !strings.Contains(string(page), `"status":"no_match"`) {
		t.Fatal("unsafe source name or missing no-match status")
	}
}
