package search

import (
	"context"
	"math"
	"testing"

	"pinpoint/internal/acoustic"
	"pinpoint/internal/geo"
	"pinpoint/internal/tracks"
)

func TestLockedCPAIntervalsRequireCommonClockAndDistinctFlights(t *testing.T) {
	if sharedCPA([][]cpaInterval{{{"A", 1, 3}}, {{"B", 4, 5}}}) {
		t.Fatal("independently shifted CPA clocks accepted")
	}
	if !sharedCPA([][]cpaInterval{{{"A", 1, 3}}, {{"B", 2, 4}}}) {
		t.Fatal("valid delayed-CPA offset rejected")
	}
	if sharedCPA([][]cpaInterval{{{"A", 1, 3}}, {{"A", 2, 4}}}) {
		t.Fatal("same track counted as two flights")
	}
	choices := [][]Match{{{Track: "A", Score: 10}, {Track: "B", Score: 9}}, {{Track: "A", Score: 11}, {Track: "C", Score: 7}}}
	got := distinctMatches(choices, 5)
	if len(got) != 2 || got[0].Track != "B" || got[1].Track != "A" {
		t.Fatalf("missed best distinct pair: %+v", got)
	}
}

func TestMeasuredCPAUsesHeardTimeAndSilenceUsesSharedClock(t *testing.T) {
	e := acoustic.Event{Start: 10, End: 50, CPATime: 30, CPATolerance: 1}
	a := tracks.Approach{Emitted: 20, Heard: 30}
	if !eventAtClock(e, a, 0) || eventAtClock(e, a, 3) {
		t.Fatal("CPA time ignored uncertainty/shared clock")
	}
	a.Heard = 20
	if eventAtClock(e, a, 0) {
		t.Fatal("used emission time instead of delayed CPA")
	}
	p := observer{pos: geo.Vec{}}
	tr := tracks.Track{ID: "extra", Points: []tracks.Point{{Time: 0, Position: geo.Vec{X: -1000, Y: 343}}, {Time: 20, Position: geo.Vec{X: 1000, Y: 343}}}}
	c := DefaultConfig()
	c.Silence = []SilentInterval{{Start: 10.5, End: 11.5, MaxSlant: 500}}
	if silenceAllows(c, []tracks.Track{tr}, p, 0) {
		t.Fatal("ignored audible delayed CPA in reviewed silence")
	}
	if !silenceAllows(c, []tracks.Track{tr}, p, 5) {
		t.Fatal("silence did not use common clock")
	}
	c.Silence[0].MaxSlant = 100
	if !silenceAllows(c, []tracks.Track{tr}, p, 0) {
		t.Fatal("ignored audibility distance")
	}
}

func TestStagedSearchScoresGeometrySurvivorsEvenWithoutCoarseAcousticPass(t *testing.T) {
	_, c, s, ts, epoch := benchmarkPoint()
	c.Grid = 200
	c.DenseGrid = 50
	c.Refine = 0
	c.Staged = true
	c.ClockUncertainty = 0
	c.Bounds = geo.Bounds{South: -.001, North: .001, West: -.001, East: .001}
	e := c.Events[0]
	e.Partial = false
	e.CPATime = 20 + 1500.0/343
	e.CPATolerance = 2
	c.Events = []acoustic.Event{e, e}
	ts = ts[:1]
	second := ts[0]
	second.ID = "second-aircraft"
	ts = append(ts, second)
	// A high threshold makes all spectra insufficient. Geometry must still
	// advance and the dense low scores must remain available for inspection.
	c.MinScore = 100
	r, err := Run(context.Background(), c, s, ts)
	if err != nil {
		t.Fatal(err)
	}
	if r.GeometryEvaluated == 0 || r.GeometrySurvived == 0 || r.Evaluated == 0 || r.DenseGrid != 50 || r.Status != "no_match" {
		t.Fatalf("coarse audio improperly gated dense search: %+v", r)
	}
	for _, p := range r.ScoreGrid {
		if p.Stage != "dense" || p.Spacing != 50 {
			t.Fatal("dense score grid mislabeled")
		}
	}
	observer := observer{pos: geo.ECEF(0, 0, 0)}
	intervals := geometryIntervals(observer, c, ts, epoch, 0)
	if !sharedCPA(intervals) {
		t.Fatal("physical CPA origin rejected")
	}
	// The two observed CPA times can each align independently, but not together.
	c.Events[1].CPATime += 8
	if sharedCPA(geometryIntervals(observer, c, ts, epoch, 0)) {
		t.Fatal("inconsistent measured separation accepted")
	}
	if math.IsNaN(r.ScoreGrid[0].Lat) {
		t.Fatal("invalid surviving grid")
	}
}
