package search

import (
	"context"
	"fmt"
	"pinpoint/internal/acoustic"
	"pinpoint/internal/geo"
	"pinpoint/internal/tracks"
	"testing"
	"time"
)

func automaticFixture() (Config, *acoustic.Spectrum, []tracks.Track) {
	c, s, ts, _, _ := timingFixture()
	for i := range c.Events {
		c.Events[i].Evidence = &acoustic.CurveEvidence{Level: "strong", Predictive: true}
		c.Events[i].Measurement = &acoustic.RidgeMeasurement{ShapePass: true}
	}
	extra := c.Events[0]
	extra.Start = 130.5
	extra.End = 149.5
	extra.CPATime = 140
	extra.FMin = 500
	extra.FMax = 800
	extra.Ridge = nil
	for sec := extra.Start; sec <= extra.End; sec++ {
		extra.Ridge = append(extra.Ridge, acoustic.RidgeSample{Time: sec, Hz: 650 - sec})
	}
	c.Events = append(c.Events, extra)
	for i := range ts {
		a, b := ts[i].Points[0], ts[i].Points[1]
		v := b.Position.Sub(a.Position).Scale(1 / (b.Time - a.Time))
		ts[i].Points = nil
		for sec := -600.; sec <= 600; sec += 2 {
			p := a.Position.Add(v.Scale(1700000000 + sec - a.Time))
			lat, lon, h := geo.Geodetic(p)
			ts[i].Points = append(ts[i].Points, tracks.Point{Time: 1700000000 + sec, Lat: lat, Lon: lon, Alt: h, Position: p})
		}
	}
	return c, s, ts
}

func TestAutomaticGroupsKeepRealPairWithoutRequiringUnrelatedCurve(t *testing.T) {
	c, s, ts := automaticFixture()
	o := DefaultAutomaticOptions(c)
	o.ControlShifts = []float64{-100, 100}
	r, err := RunAutomatic(context.Background(), c, s, ts, o, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != "candidate_sequence" || r.Best == nil || r.Best.Events != [2]int{0, 1} || len(r.Results) != 9 {
		t.Fatalf("lost correct automatic sequence: %+v", r)
	}
	for _, g := range r.Results {
		if g.Trial > 0 && g.Best != nil {
			t.Fatal("wrong-time search matched synthetic sequence")
		}
	}
}

func TestAutomaticControlsRepeatEveryPairAndUseGlobalMaximum(t *testing.T) {
	c, s, ts := automaticFixture()
	o := DefaultAutomaticOptions(c)
	o.ControlShifts = []float64{-100, 100}
	calls := 0
	r, err := runAutomatic(context.Background(), c, s, ts, o, nil, nil, func(q Config, _ func(string, int, int)) (Report, error) {
		calls++
		at, _ := time.Parse(time.RFC3339, q.RecordingStart)
		score := 10.
		if at.Unix() > 1700000000 && q.Events[0].Start == c.Events[1].Start {
			score = 12
		}
		return Report{Status: "candidate_matches", Candidates: []Candidate{{Score: score}}}, nil
	})
	if err != nil || calls != 9 || r.Status != "unresolved" || r.ControlMax == nil || *r.ControlMax != 12 {
		t.Fatalf("control did not repeat global selection: %+v %v calls %d", r, err, calls)
	}
	calls = 0
	r, err = runAutomatic(context.Background(), c, s, ts, o, nil, nil, func(q Config, _ func(string, int, int)) (Report, error) {
		calls++
		if calls == 2 {
			return Report{}, fmt.Errorf("unavailable tile")
		}
		return Report{Status: "candidate_matches", Candidates: []Candidate{{Score: 20}}}, nil
	})
	if err != nil || r.Status != "incomplete" {
		t.Fatal("incomplete candidate search claimed validation")
	}
	o.MaxPairs = 1
	if _, err = RunAutomatic(context.Background(), c, s, ts, o, nil, nil); err == nil {
		t.Fatal("silently truncated pair search")
	}
}

func TestAutomaticCacheBindsSoundTracksAndSettings(t *testing.T) {
	c, s, ts := automaticFixture()
	ctx := context.Background()
	key, err := automaticFingerprint(ctx, s, ts)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	calls := 0
	run := func() (Report, error) { calls++; return Report{Status: "no_match"}, nil }
	for i := 0; i < 2; i++ {
		if _, err = cachedAutomaticRun(ctx, dir, key, c, run); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 1 {
		t.Fatal("did not resume completed search")
	}
	c.ClockUncertainty++
	cachedAutomaticRun(ctx, dir, key, c, run)
	if calls != 2 {
		t.Fatal("reused different clock configuration")
	}
	s.Rows[0][0] = 42
	other, _ := automaticFingerprint(ctx, s, ts)
	if key == other {
		t.Fatal("ignored measured sound change")
	}
	ts[0].Points[0].Time += .01
	newKey, _ := automaticFingerprint(ctx, s, ts)
	if other == newKey {
		t.Fatal("ignored telemetry time change")
	}
}

func TestAutomaticAmbiguityIncludesSameAircraftMirrorAndClock(t *testing.T) {
	best := Candidate{Lat: 35, Lon: -100, Score: 10}
	score, clock := 9.5, 0.
	r := Report{Candidates: []Candidate{best}, ScoreGrid: []ScorePoint{{Lat: 35, Lon: -99.97, Score: &score, ClockOffset: &clock, MeetsThreshold: true}}}
	if !automaticAmbiguity(r) {
		t.Fatal("same-pair mirror disappeared")
	}
	r.ScoreGrid[0].Lon = -100
	clock = 6
	if !automaticAmbiguity(r) {
		t.Fatal("competing common clock disappeared")
	}
	clock = 0
	score = 8
	if automaticAmbiguity(r) {
		t.Fatal("accepted a distant low-score fit as a tie")
	}
	c, s, ts := automaticFixture()
	o := DefaultAutomaticOptions(c)
	o.ControlShifts = []float64{-100, 100}
	result, err := runAutomatic(context.Background(), c, s, ts, o, nil, nil, func(q Config, _ func(string, int, int)) (Report, error) {
		return Report{Candidates: []Candidate{best}, Ambiguity: "Two geographic peaks"}, nil
	})
	if err != nil || !result.Ambiguous || result.Status != "unresolved" {
		t.Fatal("ambiguous pair became a location", err)
	}
}

func TestAutomaticHonorsReviewedSilenceDespiteUnrelatedTonalProposal(t *testing.T) {
	c, s, ts := automaticFixture()
	c.Silence = []SilentInterval{{Start: 130, End: 149.5, MaxSlant: 100}}
	if _, err := c.Validate(s); err == nil {
		t.Fatal("reviewed mandatory flyover cannot overlap declared absence")
	}
	o := DefaultAutomaticOptions(c)
	o.ControlShifts = []float64{-100, 100}
	r, err := RunAutomatic(context.Background(), c, s, ts, o, nil, nil)
	if err != nil || r.Status != "candidate_sequence" || r.Pairs != 1 || len(r.Results) != 3 || len(r.ExcludedCurves) != 1 || r.ExcludedCurves[0].Index != 2 {
		t.Fatalf("unused tonal proposal defeated reviewed silence: %+v %v", r, err)
	}
}
