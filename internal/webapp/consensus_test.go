package webapp

import (
	"testing"

	"pinpoint/internal/acoustic"
	"pinpoint/internal/geo"
	"pinpoint/internal/search"
)

func consensusTestEvent(start, hz float64, level string) acoustic.Event {
	e := acoustic.Event{Start: start, End: start + 60, Evidence: &acoustic.CurveEvidence{Level: level}}
	for t := start; t < start+60; t += 2 {
		e.Ridge = append(e.Ridge, acoustic.RidgeSample{Time: t, Hz: hz})
	}
	return e
}

func TestConsensusModeValidatesStationaryAndEnoughCurves(t *testing.T) {
	s, _ := testServer(t)
	j, _ := s.newJob("consensus.wav")
	j.update(func(i *JobInfo) {
		i.Events = []acoustic.Event{consensusTestEvent(10, 120, "strong"), consensusTestEvent(300, 6800, "strong"), consensusTestEvent(600, 90, "review")}
	})
	h := 0.0
	r := defaultRequest()
	r.MatchMode = "consensus"
	r.RecordingStart = "2026-11-05T16:17:58Z"
	r.State = "TX"
	r.Bounds = geo.Bounds{South: 29, North: 30, West: -99, East: -98}
	r.Height = &h
	spectrum := &acoustic.Spectrum{}
	if _, err := s.configuration(j, r, spectrum); err == nil {
		t.Fatal("accepted consensus with one plausible strong curve")
	}
	j.update(func(i *JobInfo) { i.Events = append(i.Events, consensusTestEvent(900, 300, "strong")) })
	c, err := s.configuration(j, r, spectrum)
	if err != nil || len(c.Events) != 2 {
		t.Fatal("two strong, plausible curves should be offered", err, len(c.Events))
	}
	r.Moving = true
	if _, err := s.configuration(j, r, spectrum); err == nil {
		t.Fatal("accepted consensus for a moving receiver")
	}
	// A reviewed selection is used as given, including non-strong curves.
	r.Moving, r.Automatic = false, false
	r.Events = []acoustic.Event{consensusTestEvent(10, 120, "review"), consensusTestEvent(600, 90, "review")}
	events, info := consensusCurves(r, nil)
	if len(events) != 2 || info[0].Index != 1 || info[1].Index != 2 {
		t.Fatal("reviewed curves not offered", info)
	}
}

func TestConsensusOptionsScaleGridAndClock(t *testing.T) {
	c := search.DefaultConfig()
	c.RecordingStart = "2026-11-05T16:17:58Z"
	c.ClockUncertainty = 120
	c.Bounds, _ = geo.StateBounds("TX")
	o := consensusOptions(c, 3)
	if o.Grid != 1000 || o.ClockStep != 4 || o.FineClockStep != 1 || o.Workers != 3 {
		t.Fatal("statewide options", o.Grid, o.ClockStep, o.FineClockStep, o.Workers)
	}
	c.Bounds = geo.Bounds{South: 35.1, North: 35.3, West: -100.6, East: -100.4}
	c.ClockUncertainty = 6
	if o = consensusOptions(c, 3); o.Grid != 250 || o.ClockStep != 2 {
		t.Fatal("small-area options", o.Grid, o.ClockStep)
	}
}
