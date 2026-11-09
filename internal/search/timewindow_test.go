package search

import (
	"context"
	"math"
	"pinpoint/internal/acoustic"
	"pinpoint/internal/geo"
	"pinpoint/internal/tracks"
	"testing"
)

func TestFlightWindowIncludesWholeRecordingNotSelectedEvent(t *testing.T) {
	c, s, _ := scoreGridFixture()
	c.ClockUncertainty = 30
	// Extend the recording to an hour while keeping one short early event.
	s.Rows = make([][]float32, 3600)
	from, until := RecordingFlightWindow(c, s)
	if from > 1700000000-30-150 || until < 1700000000+3600+30 {
		t.Fatalf("history truncated: %f..%f", from, until)
	}
	c.Events = nil
	a, b := RecordingFlightWindow(c, s)
	if a != from || b != until {
		t.Fatal("history depends on detected event selection")
	}
}

func TestTimeTrimmingRetainsBoundarySegmentsAndAcousticDelay(t *testing.T) {
	c, _, _ := scoreGridFixture()
	c.Events[0].Start = 100
	c.Events[0].End = 130
	c.Events[0].MaxSlant = 15000
	tr := tracks.Track{ID: "long-flight"}
	for sec := -100; sec <= 400; sec += 10 {
		tr.Points = append(tr.Points, tracks.Point{Time: 1700000000 + float64(sec)})
	}
	got := TracksForEvents(c, []tracks.Track{tr, {ID: "irrelevant", Points: []tracks.Point{{Time: 1}, {Time: 2}}}})
	if len(got) != 1 || len(got[0].Points) >= len(tr.Points) {
		t.Fatal("did not trim irrelevant history")
	}
	p := got[0].Points
	if p[0].Time > 1700000000+100-15000/343.0-10 || p[len(p)-1].Time < 1700000000+140 {
		t.Fatal("lost interpolation boundaries")
	}
}

func TestPartialFlyoverOutsideCPAIsMatchedAgainstObservedRidge(t *testing.T) {
	c, s, _ := scoreGridFixture()
	const speed = 80.0
	const height = 1200.0
	const cpa = 120.0
	const sound = 343.0
	const f0 = 130.0
	s.Rows = make([][]float32, 101)
	e := c.Events[0]
	e.Start = 40.5
	e.End = 65.5
	e.CPATime = 50
	e.FMin = 80
	e.FMax = 200
	e.Partial = true
	for frame := range s.Rows {
		row := make([]float32, 200)
		s.Rows[frame] = row
		heard := s.Time(frame) - cpa
		emitted := (sound*sound*heard - math.Sqrt(sound*sound*speed*speed*heard*heard+(sound*sound-speed*speed)*height*height)) / (sound*sound - speed*speed)
		hz := f0 * sound / (sound + speed*speed*emitted/math.Hypot(height, speed*emitted))
		for k := range row {
			delta := (s.Hz(k) - hz) / .8
			row[k] = float32(20 * math.Exp(-delta*delta))
		}
		if s.Time(frame) >= e.Start && s.Time(frame) <= e.End {
			e.Ridge = append(e.Ridge, acoustic.RidgeSample{Time: s.Time(frame), Hz: hz})
		}
	}
	c.Events = []acoustic.Event{e}
	tr := tracks.Track{ID: "late-cpa"}
	origin := geo.ECEF(0, 0, 0)
	for sec := -60; sec <= 180; sec += 2 {
		pos := origin.Add(geo.Vec{X: height, Y: (float64(sec) - cpa) * speed})
		lat, lon, alt := geo.Geodetic(pos)
		tr.Points = append(tr.Points, tracks.Point{Time: 1700000000 + float64(sec), Position: pos, Lat: lat, Lon: lon, Alt: alt})
	}
	ts := []tracks.Track{tr}
	report, err := Run(context.Background(), c, s, ts)
	if err != nil || report.Status != "candidate_matches" {
		t.Fatalf("partial event wrongly rejected: %s %v", report.Status, err)
	}
	c.Events[0].Partial = false
	report, err = Run(context.Background(), c, s, ts)
	if err != nil || report.Status != "no_match" {
		t.Fatal("complete-CPA constraint was not retained")
	}
	c.Events[0].Partial = true
	// Wrong trajectory curvature must not pass just by finding unrelated bright bins.
	for k := range e.Ridge {
		c.Events[0].Ridge[k].Hz += float64(k) * 2
	}
	report, err = Run(context.Background(), c, s, ts)
	if err != nil || report.Status != "no_match" {
		t.Fatal("accepted a trajectory inconsistent with the measured ridge")
	}
}
