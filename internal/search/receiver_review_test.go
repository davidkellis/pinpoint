package search

import (
	"context"
	"math"
	"testing"
)

func TestReceiverReviewFindsSoundWithoutInventingAnAnchor(t *testing.T) {
	c, s, ts, seeds, o := guidedFixture(t)
	h := ReceiverHypothesis{Lat: seeds[0].Lat, Lon: seeds[0].Lon, Height: seeds[0].Height, Clock: seeds[0].ClockOffset, RecordingStart: c.RecordingStart, SoundSpeed: c.SoundSpeed, MaxGap: c.MaxGap}
	r, err := ReviewReceiver(context.Background(), h, s, ts[1:], o)
	if err != nil {
		t.Fatal(err)
	}
	if r.Receiver != h || r.Supported != 1 || len(r.Matches) == 0 || r.Matches[0].Track != "east-west" || r.Matches[0].AnchorGap != 0 {
		t.Fatalf("supplied receiver review changed context or required an anchor: %+v", r)
	}
	// A source's complete absence must not gain support from its predicted path.
	for _, row := range s.Rows {
		clear(row)
	}
	r, err = ReviewReceiver(context.Background(), h, s, ts[1:], o)
	if err != nil || r.Supported != 0 {
		t.Fatalf("predicted flight invented audio evidence: %+v %v", r, err)
	}
}

func TestReceiverReviewRejectsInvalidContextAndCancels(t *testing.T) {
	c, s, ts, seeds, o := guidedFixture(t)
	h := ReceiverHypothesis{Lat: seeds[0].Lat, Lon: seeds[0].Lon, Height: seeds[0].Height, RecordingStart: c.RecordingStart, SoundSpeed: c.SoundSpeed, MaxGap: c.MaxGap}
	bad := h
	bad.Clock = math.NaN()
	if _, err := ReviewReceiver(context.Background(), bad, s, ts, o); err == nil {
		t.Fatal("accepted nonfinite clock")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ReviewReceiver(ctx, h, s, ts, o); err != context.Canceled {
		t.Fatalf("ignored cancellation: %v", err)
	}
}

func TestReceiverReviewKeepsOneClockForAllFlights(t *testing.T) {
	c, s, ts, seeds, o := guidedFixture(t)
	// Keep both passages away from the start boundary so a clock shift does
	// not legitimately remove one for lack of a complete review window.
	padding := make([][]float32, 100)
	for i := range padding {
		padding[i] = make([]float32, len(s.Rows[0]))
	}
	s.Rows = append(padding, s.Rows...)
	for i := range ts {
		for j := range ts[i].Points {
			ts[i].Points[j].Time += 100
		}
	}
	h := ReceiverHypothesis{Lat: seeds[0].Lat, Lon: seeds[0].Lon, Height: seeds[0].Height, RecordingStart: c.RecordingStart, SoundSpeed: c.SoundSpeed, MaxGap: c.MaxGap}
	a, err := ReviewReceiver(context.Background(), h, s, ts, o)
	if err != nil {
		t.Fatal(err)
	}
	h.Clock = 10
	b, err := ReviewReceiver(context.Background(), h, s, ts, o)
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, before := range a.Matches {
		for _, after := range b.Matches {
			if before.Track == after.Track {
				if math.Abs(before.CPA-after.CPA-10) > 1e-6 {
					t.Fatalf("flight %s was shifted independently", before.Track)
				}
				checked++
				break
			}
		}
	}
	if checked < 2 || b.Receiver != h {
		t.Fatal("lost the shared-clock schedule or changed the supplied receiver")
	}
}

func TestReceiverReviewIntervalsRestrictFitsButPreserveWholeAudioControls(t *testing.T) {
	c, s, ts, seeds, o := guidedFixture(t)
	h := ReceiverHypothesis{Lat: seeds[0].Lat, Lon: seeds[0].Lon, Height: seeds[0].Height, RecordingStart: c.RecordingStart, SoundSpeed: c.SoundSpeed, MaxGap: c.MaxGap}
	baseline, err := ReviewReceiver(context.Background(), h, s, ts[1:], o)
	if err != nil || len(baseline.Matches) == 0 {
		t.Fatal("lost fixture", err)
	}
	m := baseline.Matches[0]
	o.ReviewIntervals = []ReviewInterval{{Start: m.Start + 1, End: m.End - 1}}
	r, err := ReviewReceiver(context.Background(), h, s, ts[1:], o)
	if err != nil || len(r.Matches) == 0 {
		t.Fatal("lost restricted match", err)
	}
	for _, m := range r.Matches {
		if m.Fit.Valid && (m.Interval != 1 || m.Start < o.ReviewIntervals[0].Start || m.End > o.ReviewIntervals[0].End || len(m.Controls) < 3) {
			t.Fatal("cropped the wrong-time control audio or escaped review interval", m)
		}
	}
	o.ReviewIntervals = []ReviewInterval{{Start: 10, End: 20}, {Start: 15, End: 30}}
	if _, err := ReviewReceiver(context.Background(), h, s, ts, o); err == nil {
		t.Fatal("accepted overlapping observations")
	}
}

func TestTrainingOnlyReviewFreezesSameAircraftPitchAndWindowBeforeConfirmation(t *testing.T) {
	c, s, ts, seeds, o := guidedFixture(t)
	h := ReceiverHypothesis{Lat: seeds[0].Lat, Lon: seeds[0].Lon, Height: seeds[0].Height, RecordingStart: c.RecordingStart, SoundSpeed: c.SoundSpeed, MaxGap: c.MaxGap}
	a, err := ReviewReceiver(context.Background(), h, s, ts[1:], o)
	if err != nil {
		t.Fatal(err)
	}
	o.TrainingOnly = true
	b, err := ReviewReceiver(context.Background(), h, s, ts[1:], o)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Matches) != len(b.Matches) || len(a.Matches) == 0 || b.Supported != 0 {
		t.Fatal("changed training hypotheses or asserted confirmation")
	}
	for i, m := range b.Matches {
		p := a.Matches[i]
		if m.Track != p.Track || m.Start != p.Start || m.End != p.End || m.Fit.F0 != p.Fit.F0 || m.Fit.Selection != p.Fit.Selection || m.Supported || len(m.Controls) > 0 || m.Validation != "not_evaluated_training_only" {
			t.Fatal("training-only pass changed selection or ran confirmation", m, p)
		}
	}
}
