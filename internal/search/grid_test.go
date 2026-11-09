package search

import (
	"context"
	"pinpoint/internal/geo"
	"reflect"
	"testing"
)

func TestCorridorGridPreservesSmallSearchScores(t *testing.T) {
	c, s, ts := scoreGridFixture()
	for i := range ts[0].Points {
		p := &ts[0].Points[i]
		p.Lat, p.Lon, p.Alt = geo.Geodetic(p.Position)
	}
	for _, row := range s.Rows {
		for i := range row {
			row[i] = 8
		}
	}
	a, e := Run(context.Background(), c, s, ts)
	if e != nil {
		t.Fatal(e)
	}
	c.Corridors = true
	b, e := Run(context.Background(), c, s, ts)
	if e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(a, b) {
		t.Fatal("corridor pruning changed in-range scores")
	}
	for i := range ts[0].Points {
		ts[0].Points[i].Time += 10000
	}
	points, e := GridLocations(context.Background(), c, ts)
	if e != nil || len(points) != 0 {
		t.Fatal("irrelevant-time corridors retained")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c.Corridors = false
	if _, e = GridLocations(ctx, c, nil); e == nil {
		t.Fatal("ignored cancellation")
	}
}

func TestDistantNearTiesAreFlaggedWithoutClaimingProbability(t *testing.T) {
	r := Report{FlightGroups: []Candidate{{Lat: 32.6, Lon: -97.6, Score: 9}, {Lat: 27.5, Lon: -99.5, Score: 8.8}}}
	AssessAmbiguity(&r)
	if r.Ambiguity == "" {
		t.Fatal("distant near-tie presented as a single answer")
	}
	r = Report{FlightGroups: []Candidate{{Lat: 32.6, Lon: -97.6, Score: 9}, {Lat: 32.6001, Lon: -97.6, Score: 8.8}}}
	AssessAmbiguity(&r)
	if r.Ambiguity != "" {
		t.Fatal("neighboring grid points treated as distant hypotheses")
	}
}

func TestSameAircraftDistantPeakIsNotHiddenByGrouping(t *testing.T) {
	best := Candidate{Lat: 35.4, Lon: -100.2, Score: 15.5, Events: []Match{{Track: "one-aircraft"}}}
	score := 14.9
	r := Report{FlightGroups: []Candidate{best}, Candidates: []Candidate{best},
		ScoreGrid: []ScorePoint{{Lat: 35.54, Lon: -100.35, Score: &score, MeetsThreshold: true, TrackIDs: []string{"one-aircraft"}}}}
	AssessAmbiguity(&r)
	if r.Ambiguity == "" {
		t.Fatal("one flight group concealed a distant competing position")
	}
	r.Ambiguity = ""
	r.ScoreGrid[0].MeetsThreshold = false
	AssessAmbiguity(&r)
	if r.Ambiguity != "" {
		t.Fatal("a rejected fit should not establish a competing location")
	}
}
