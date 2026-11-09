package search

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"pinpoint/internal/acoustic"
	"pinpoint/internal/tracks"
)

func sequenceFixture(t *testing.T) (Config, *acoustic.Spectrum, []tracks.Track, []Candidate, GuidedOptions) {
	t.Helper()
	c, s, ts, seeds, o := guidedFixture(t)
	rows := make([][]float32, 2400)
	for i := range rows {
		rows[i] = make([]float32, len(s.Rows[0]))
	}
	for i, row := range s.Rows {
		shift := 1000
		if s.Time(i) < 60 {
			shift = 400
		}
		copy(rows[i+shift], row)
	}
	s.Rows = rows
	c.Events[0].Start += 400
	c.Events[0].End += 400
	c.Events[0].CPATime += 400
	for i := range c.Events[0].Ridge {
		c.Events[0].Ridge[i].Time += 400
	}
	for j := range ts {
		shift := 1000.
		if j == 0 {
			shift = 400
		}
		for i := range ts[j].Points {
			ts[j].Points[i].Time += shift
		}
	}
	for i := range seeds {
		seeds[i].Events[0].CPA.Emitted += 400
		seeds[i].Events[0].CPA.Heard += 400
	}
	o.SequenceControls = 5
	return c, s, ts, seeds, o
}

func TestWholeSequenceTimingControl(t *testing.T) {
	c, s, ts, seeds, o := sequenceFixture(t)
	r, err := Corroborate(context.Background(), c, s, ts, seeds, o, nil)
	if err != nil {
		t.Fatal(err)
	}
	a := r.Sequence
	if a == nil || a.Best == nil || !a.Separated || a.Conclusion != "passes_timing_controls_unverified" {
		t.Fatalf("correct sequence lost: %+v", a)
	}
	best := a.Candidates[*a.Best]
	if best.Seed.Lon != seeds[0].Lon || best.Seed.ClockOffset != seeds[0].ClockOffset || best.Actual.Events[0].Track != "east-west" {
		t.Fatal("changed receiver/clock or picked wrong aircraft")
	}
	for _, p := range a.Candidates {
		for _, control := range p.Controls {
			if control.Score.Attempted != p.Actual.Attempted {
				t.Fatal("unequal encounter opportunities across schedules")
			}
		}
	}
	dir := t.TempDir()
	if err = WriteGuidedReport(dir, r, s); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "guided.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "Whole-sequence timing audit") || strings.Contains(string(b), "%!") {
		t.Fatal("broken sequence report")
	}
	// A second call with a different worker count must make identical decisions.
	o.Workers = 1
	r2, err := Corroborate(context.Background(), c, s, ts, seeds, o, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(r.Sequence, r2.Sequence) {
		t.Fatal("worker scheduling changed the audit")
	}
}

func TestSequenceRejectsChangedRelativeTimingAndMissingSound(t *testing.T) {
	for _, kind := range []string{"wrong_gap", "missing", "steady"} {
		t.Run(kind, func(t *testing.T) {
			c, s, ts, seeds, o := sequenceFixture(t)
			if kind == "wrong_gap" {
				for i := range ts[1].Points {
					ts[1].Points[i].Time += 20
				}
			} else {
				for i, row := range s.Rows {
					if s.Time(i) > c.Events[0].End+2 {
						clear(row)
						if kind == "steady" {
							row[110] = 30
						}
					}
				}
			}
			r, err := Corroborate(context.Background(), c, s, ts, seeds, o, nil)
			if err != nil {
				t.Fatal(err)
			}
			if r.Sequence.Separated {
				t.Fatalf("accepted %s sequence", kind)
			}
		})
	}
}

func TestSequenceRequiresEveryAdditionalFlightToAgree(t *testing.T) {
	c, s, ts, seeds, o := sequenceFixture(t)
	o.SequenceExtra = 2
	third := ts[1]
	third.ID = "third-aircraft"
	third.Points = append([]tracks.Point(nil), third.Points...)
	for i := range third.Points {
		third.Points[i].Time += 500
	}
	ts = append(ts, third)
	for i := 1070; i < 1140; i++ {
		copy(s.Rows[i+500], s.Rows[i])
	}
	r, err := Corroborate(context.Background(), c, s, ts, seeds, o, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Sequence.Separated || len(r.Sequence.Candidates[*r.Sequence.Best].Actual.Events) != 2 {
		t.Fatal("lost valid three-aircraft sequence")
	}
	for i := range ts[2].Points {
		ts[2].Points[i].Time += 20
	}
	r, err = Corroborate(context.Background(), c, s, ts, seeds, o, nil)
	if err != nil {
		t.Fatal(err)
	}
	if r.Sequence.Separated {
		t.Fatal("strong second flight hid the third flight's wrong timing")
	}
}

func TestSequenceSelectionCannotCherryPickHeldoutFlight(t *testing.T) {
	c, s, _, _, o := sequenceFixture(t)
	makeMatch := func(id string, train, test float64) GuidedMatch {
		return GuidedMatch{Track: id, CPA: 1100, Start: 1080, End: 1120, Fit: GuidedFit{Valid: true, Train: train, Heldout: test, Positive: 1}, Flat: GuidedFit{Valid: true}, Curve: []acoustic.RidgeSample{{Time: 1080, Hz: 150}, {Time: 1120, Hz: 120}}}
	}
	ms := []GuidedMatch{makeMatch("training-choice", 12, -2), makeMatch("test-winner", 8, 20)}
	r := selectSequence(c, s, ms, o, sequenceShifts(5), 0)
	if !r.Valid || r.Events[0].Track != "training-choice" || r.Quality {
		t.Fatal("selected using heldout evidence")
	}
	ms[1].Fit.Heldout = math.Inf(1)
	r = selectSequence(c, s, ms, o, sequenceShifts(5), 0)
	if r.Events[0].Track != "training-choice" {
		t.Fatal("heldout score influenced training selection")
	}
}

func TestSequenceControlsUseWholeSearchMaximum(t *testing.T) {
	o := DefaultGuidedOptions()
	score := func(x float64) SequenceScore { return SequenceScore{Valid: true, Quality: true, Test: x} }
	a := SequenceAudit{Shifts: []float64{140, 160, 180, 200, 220}}
	for i := 0; i < 2; i++ {
		p := SequenceCandidate{Actual: score(10 - float64(i))}
		for _, shift := range a.Shifts {
			p.Controls = append(p.Controls, SequenceControl{shift, score(float64(i) * 11)})
		}
		a.Candidates = append(a.Candidates, p)
	}
	finishSequenceAudit(&a, o)
	if a.Separated || a.ControlMargin == nil || *a.ControlMargin != -1 || a.Maxima[0].Candidate != 1 {
		t.Fatal("ignored a stronger control at a competing location")
	}
}

func TestSequenceEligibilityNeverUsesAnchorOrTruncatedControls(t *testing.T) {
	c, s, _, _, _ := sequenceFixture(t)
	shifts := sequenceShifts(19)
	if !sequenceEligible(c, s, 1100, shifts) {
		t.Fatal("lost safe interior encounter")
	}
	for _, center := range []float64{200, 420, 2300} {
		if sequenceEligible(c, s, center, shifts) {
			t.Fatal("admitted anchor/edge-contaminated control")
		}
	}
}

func TestFollowupPoolPreservesSpatialAndClockAlternatives(t *testing.T) {
	var in []Candidate
	for i := 0; i < 100; i++ {
		in = append(in, Candidate{Lat: float64(i) * 1e-7, Score: 20 - float64(i)*.001, Events: []Match{{Track: "a"}}})
	}
	in = append(in, Candidate{Lat: .02, Score: 18, Events: []Match{{Track: "a"}}}, Candidate{ClockOffset: 20, Score: 17, Events: []Match{{Track: "a"}}}, Candidate{Score: 16, Events: []Match{{Track: "b"}}})
	out := followupSeeds(in, 500, 5, 8)
	if len(out) != 4 {
		t.Fatalf("dense cluster consumed pool: %+v", out)
	}
	var spatial, clock, other bool
	for _, p := range out {
		spatial = spatial || p.Lat == .02
		clock = clock || p.ClockOffset == 20
		other = other || p.Events[0].Track == "b"
	}
	if !spatial || !clock || !other {
		t.Fatal("discarded competing hypothesis")
	}
}
