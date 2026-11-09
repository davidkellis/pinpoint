package search

import (
	"context"
	"math"
	"pinpoint/internal/acoustic"
	"pinpoint/internal/geo"
	"pinpoint/internal/tracks"
	"testing"
)

func TestEvidenceShowsClockInvariantArrivalGapAndPartialSampleTiming(t *testing.T) {
	c, s, ts, truth, _ := timingFixture()
	best, _, ok := evaluatePoint(context.Background(), truth, c, s, ts, 1700000000)
	if !ok {
		t.Fatal("fixture lost its physical match")
	}
	for i := range c.Events {
		c.Events[i].Partial = false
		c.Events[i].CPATime = best.Events[i].CPA.Heard - 1700000000
		c.Events[i].CPATolerance = 2
	}
	r := Report{Candidates: []Candidate{best}}
	a, err := BuildEvidence(r, c, s, ts)
	if err != nil {
		t.Fatal(err)
	}
	gap := a.Events[1].Models[0].Timing
	if gap == nil || gap.Reference != 1 || math.Abs(gap.Residual) > 1e-8 || gap.Tolerance != 4 {
		t.Fatalf("incorrect relative arrival evidence: %+v", gap)
	}
	r.Candidates[0].ClockOffset = 4
	b, err := BuildEvidence(r, c, s, ts)
	if err != nil {
		t.Fatal(err)
	}
	if *b.Events[1].Models[0].Timing != *gap {
		t.Fatal("display changed relative spacing with common clock")
	}
	if math.Abs(a.Events[0].Models[0].CPA-b.Events[0].Models[0].CPA-4) > 1e-8 {
		t.Fatal("CPA display lost shared clock correction")
	}
	c.Events[1].Partial = true
	c.Events[1].CPATolerance = 0
	p, err := BuildEvidence(r, c, s, ts)
	if err != nil {
		t.Fatal(err)
	}
	if p.Events[1].Models[0].Timing != nil || p.Events[1].StartOffset != c.Events[1].Start-c.Events[0].Start {
		t.Fatal("invented partial arrival CPA or lost measured relative sample timing")
	}
}

func TestEvidenceKeepsSharedClockAndUsesRetardedFlightGeometry(t *testing.T) {
	c, s, _ := scoreGridFixture()
	c.Events = append(c.Events, c.Events[0])
	c.Events[1].Start = 15.5
	c.Events[1].End = 35.5
	observer := geo.ECEF(0, 0, 0)
	tr := tracks.Track{ID: "physical-track"}
	for sec := -100; sec <= 100; sec += 2 {
		tr.Points = append(tr.Points, tracks.Point{Time: 1700000000 + float64(sec), Position: observer.Add(geo.Vec{X: 1200, Y: 80 * (float64(sec) - 20)})})
	}
	candidate := Candidate{Lat: 0, Lon: 0, Height: 0, ClockOffset: 3, Score: 10, Events: []Match{{Track: tr.ID, F0: 130, Score: 10}, {Track: tr.ID, F0: 160, Score: 10}}}
	r := Report{FlightGroups: []Candidate{candidate}}
	data, err := BuildEvidence(r, c, s, []tracks.Track{tr})
	if err != nil {
		t.Fatal(err)
	}
	if len(data.Events) != 2 || data.Candidates[0].ClockOffset != 3 {
		t.Fatal("lost joint comparison")
	}
	for k, event := range data.Events {
		if len(event.Models) != 1 || len(event.Models[0].Segments) == 0 {
			t.Fatal("missing predictions")
		}
		for _, p := range event.Models[0].Segments[0] {
			heard := p.Time + 3 - 20
			const v = 80.0
			const sound = 343.0
			const d = 1200.0
			te := (sound*sound*heard - math.Sqrt(sound*sound*v*v*heard*heard+(sound*sound-v*v)*d*d)) / (sound*sound - v*v)
			want := candidate.Events[k].F0 * sound / (sound + v*v*te/math.Hypot(d, v*te))
			if math.Abs(want-p.Hz) > 1e-6 {
				t.Fatal("display changed shared clock or ignored propagation")
			}
		}
	}
	c.Events = []acoustic.Event{c.Events[0]}
	empty, err := BuildEvidence(Report{}, c, s, nil)
	if err != nil || len(empty.Candidates) != 0 || len(empty.Events[0].Models) != 0 {
		t.Fatal("invented a match for a detection-only view")
	}
}

func TestReservedValidationBelongsOnlyToTheFrozenCandidate(t *testing.T) {
	c, s, ts, truth, _ := timingFixture()
	best, _, ok := evaluatePoint(context.Background(), truth, c, s, ts, 1700000000)
	if !ok {
		t.Fatal("missing physical fixture")
	}
	c.validationCandidate = &best
	for i := range c.Events {
		c.validationChecks = append(c.validationChecks, SequenceCheck{EventIndex: i, Pass: true, Plan: eventFold(c.Events[i], s)})
	}
	clock, location, tone := best, best, best
	clock.ClockOffset++
	location.Lat += .001
	tone.Events = append([]Match(nil), best.Events...)
	for i := range tone.Events {
		tone.Events[i].F0++
	}
	r := Report{Candidates: []Candidate{best, clock, location, tone}}
	evidence, err := BuildEvidence(r, c, s, ts)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range evidence.Events {
		for i, model := range event.Models {
			if (model.Validation != nil) != (i == 0) {
				t.Fatal("reserved outcome transferred to a refitted candidate")
			}
		}
	}
}
