package search

import (
	"context"
	"errors"
	"math"
	"reflect"
	"testing"

	"pinpoint/internal/acoustic"
	"pinpoint/internal/geo"
	"pinpoint/internal/tracks"
)

func automaticSequenceFixture() (Config, *acoustic.Spectrum, []tracks.Track, observer) {
	c, s, ts, truth, _ := timingFixture()
	for len(s.Rows) < 240 {
		s.Rows = append(s.Rows, make([]float32, len(s.Rows[0])))
	}
	center, tone, speed, d := 180., 185., 100., 1300.
	v := geo.Vec{Y: 60, Z: 80}
	origin := truth.pos.Add(geo.Vec{X: d})
	third := tracks.Track{ID: "third-aircraft", TypeCode: "A320"}
	for sec := -600.; sec <= 600; sec += 2 {
		p := origin.Add(v.Scale(sec - center))
		la, lo, h := geo.Geodetic(p)
		third.Points = append(third.Points, tracks.Point{Time: 1700000000 + sec, Lat: la, Lon: lo, Alt: h, Position: p})
	}
	ts = append(ts, third)
	e := acoustic.Event{Start: 160.5, End: 200.5, CPATime: center, Partial: true, FMin: 140, FMax: 240, FStep: .25, Harmonics: []int{1}, MinSpeed: 15, MaxSpeed: 300, MaxSlant: 15000}
	for i, row := range s.Rows {
		t := s.Time(i)
		if t < e.Start || t > e.End {
			continue
		}
		u := t - center
		const c = 343.
		te := (c*c*u - math.Sqrt(c*c*speed*speed*u*u+(c*c-speed*speed)*d*d)) / (c*c - speed*speed)
		hz := tone * c / (c + speed*speed*te/math.Hypot(d, speed*te))
		e.Ridge = append(e.Ridge, acoustic.RidgeSample{Time: t, Hz: hz})
		for j := range row {
			row[j] += float32(25 * math.Exp(-math.Pow((s.Hz(j)-hz)/.8, 2)))
		}
	}
	c.Events = append(c.Events, e)
	for i := range ts[:2] {
		a, b := ts[i].Points[0], ts[i].Points[1]
		v := b.Position.Sub(a.Position).Scale(1 / (b.Time - a.Time))
		ts[i].Points = nil
		for sec := -600.; sec <= 600; sec += 2 {
			p := a.Position.Add(v.Scale(1700000000 + sec - a.Time))
			la, lo, h := geo.Geodetic(p)
			ts[i].Points = append(ts[i].Points, tracks.Point{Time: 1700000000 + sec, Lat: la, Lon: lo, Alt: h, Position: p})
		}
	}
	noise := e
	noise.Start = 210.5
	noise.End = 230.5
	noise.CPATime = 220
	noise.FMin = 400
	noise.FMax = 600
	noise.Ridge = nil
	for sec := 211.; sec <= 230; sec++ {
		noise.Ridge = append(noise.Ridge, acoustic.RidgeSample{Time: sec, Hz: 500 - sec})
	}
	c.Events = append(c.Events, noise)
	for i := range c.Events {
		c.Events[i].Evidence = &acoustic.CurveEvidence{Level: "strong", Predictive: true}
		c.Events[i].Measurement = &acoustic.RidgeMeasurement{ShapePass: true}
		c.eventSpectra = append(c.eventSpectra, s)
	}
	c.ObservationFold = "train"
	c.ScoreObjective = "weakest"
	c.DistinctAircraft = true
	c.CurveEnvelope = true
	c.Refine = 0
	c.FinalGrid = 0
	return c, s, ts, truth
}

func TestAutomaticSequencesUseThreeFlightsAndLeaveUnrelatedToneUnassigned(t *testing.T) {
	c, s, ts, truth := automaticSequenceFixture()
	o := DefaultSequenceOptions(c)
	o.ControlShifts = []float64{-150, 150}
	r, err := runSequenceSearch(context.Background(), c, s, ts, o, nil, nil, func(q Config, p func(string, int, int)) (Report, error) {
		if q.Bounds != c.Bounds {
			t.Fatal("larger group search was clipped to a selected parent's position")
		}
		return RunTiled(context.Background(), q, s, ts, p)
	})
	if err != nil {
		t.Fatal(err)
	}
	if r.Selected == nil {
		t.Fatalf("no selected sequence: %+v", r)
	}
	g := r.Groups[*r.Selected]
	if !reflect.DeepEqual(g.Indices, []int{0, 1, 2}) || r.Status != "candidate_sequence" {
		t.Fatalf("lost true sequence: %s selected %+v reasons %+v", r.Status, g, r.Reasons)
	}
	if geo.ECEF(g.Best.Lat, g.Best.Lon, g.Best.Height).Sub(truth.pos).Norm() > 180 {
		t.Fatal("sequence lost known position")
	}
	if len(g.Checks) != 3 {
		t.Fatal("did not validate every selected aircraft")
	}
	for _, q := range g.Checks {
		if !q.Pass || q.TrainSamples < 5 || q.TestSamples < 5 {
			t.Fatal("invalid disjoint validation", q)
		}
	}
}

func TestSequenceBudgetAndFailedSearchCannotBecomeNoMatch(t *testing.T) {
	c, s, ts, _ := automaticSequenceFixture()
	o := DefaultSequenceOptions(c)
	o.ControlShifts = []float64{-150, 150}
	o.MaxGroups = 1
	r, err := runSequenceSearch(context.Background(), c, s, ts, o, nil, nil, func(Config, func(string, int, int)) (Report, error) {
		t.Fatal("budgeted group set should fail before any partial search")
		return Report{}, nil
	})
	if err != nil || r.Status != "incomplete" || len(r.Reasons) == 0 || r.Reasons[0].Code != "group_budget" {
		t.Fatal("silently truncated the group search", r.Status, err)
	}
	o.MaxGroups = 100
	r, err = runSequenceSearch(context.Background(), c, s, ts, o, nil, nil, func(Config, func(string, int, int)) (Report, error) {
		return Report{}, errors.New("missing test data")
	})
	if err != nil || r.Status != "incomplete" {
		t.Fatal("failed search became a completed outcome", r.Status, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r, err = runSequenceSearch(ctx, c, s, ts, o, nil, nil, func(Config, func(string, int, int)) (Report, error) { return Report{}, nil })
	if !errors.Is(err, context.Canceled) || r.Status != "incomplete" {
		t.Fatal("cancelled search became a completed outcome", r.Status, err)
	}
}

func TestSequenceMissingAudioWritesAnIncompleteDiagnostic(t *testing.T) {
	c, s, ts, _ := automaticSequenceFixture()
	o := DefaultSequenceOptions(c)
	o.AudioPath = t.TempDir() + "/missing.wav"
	r, err := RunAutomaticSequences(context.Background(), c, s, ts, o, nil, nil)
	if err == nil || r.Status != "incomplete" || r.Version == "" || r.Error == "" || r.Started == "" || r.Finished == "" {
		t.Fatal("lost preparation error or wrote a completed outcome", r.Status, err)
	}
}

func TestSequenceControlUsesWorstGroupIncludingFailedQualityGate(t *testing.T) {
	o := DefaultSequenceOptions(DefaultConfig())
	actual, a, b := 5., 2., 4.5
	r := SequenceReport{Options: o, Groups: []SequenceGroup{
		{ID: 0, Indices: []int{0, 1, 2}, Best: &Candidate{Score: 12}, Pass: true, Validation: &actual},
		{ID: 1, Trial: 1, Indices: []int{0, 1, 2}, Best: &Candidate{Score: 20}, Pass: true, Validation: &a},
		{ID: 2, Trial: 1, Indices: []int{0, 3}, Best: &Candidate{Score: 8}, Pass: false, Validation: &b},
	}}
	finishSequenceSearch(&r, false)
	if r.Status != "unresolved" || r.ControlMax == nil || *r.ControlMax != b || r.Margin == nil || *r.Margin != .5 {
		t.Fatal("selected an easier null or concealed a competing group", r)
	}
}

func TestFFTSupportDoesNotOverlapBetweenTrainingAndValidation(t *testing.T) {
	for _, resolution := range []float64{.5, 1, 2, 4} {
		e := acoustic.Event{Start: .5, End: 100.5, Resolution: resolution}
		s := &acoustic.Spectrum{SampleRate: 4096, Window: int(4096 * resolution), Hop: int(512 * resolution)}
		p := eventFold(e, s)
		var train, test []float64
		for at := e.Start; at <= e.End; at += resolution / 8 {
			if p.Contains(at, "train") {
				train = append(train, at)
			}
			if p.Contains(at, "validation") {
				test = append(test, at)
			}
		}
		if len(train) == 0 || len(test) == 0 {
			t.Fatal("fixture has no guarded frames")
		}
		for _, a := range train {
			for _, b := range test {
				if math.Abs(a-b) < resolution {
					t.Fatal("opposite FFT windows share measured samples", resolution, a, b)
				}
			}
		}
	}
}

func TestTrainingSelectionNeverDropsAnEventUsingValidationOutcome(t *testing.T) {
	o := DefaultSequenceOptions(DefaultConfig())
	v := 5.
	r := SequenceReport{Options: o, Groups: []SequenceGroup{
		{ID: 0, Indices: []int{0, 1}, Best: &Candidate{Score: 20}, Pass: true, Validation: &v},
		{ID: 1, Indices: []int{0, 1, 2}, Best: &Candidate{Score: 8}, Pass: false, Validation: &v},
	}}
	finishSequenceSearch(&r, false)
	if r.Selected == nil || *r.Selected != 1 || r.Status != "unresolved" {
		t.Fatal("peeked at validation to rescue another group")
	}
}

func TestSequenceExpansionRequiresEveryTrainingParent(t *testing.T) {
	e := make([]acoustic.Event, 5)
	for i := range e {
		e[i].Start = float64(10 * i)
		e[i].End = e[i].Start + 5
	}
	parents := map[string][]int{}
	for _, p := range [][]int{{0, 1}, {0, 2}, {1, 2}, {0, 3}} {
		parents[indexKey(p)] = p
	}
	if got := nextSequenceGroups(parents, e, nil); !reflect.DeepEqual(got, [][]int{{0, 1, 2}}) {
		t.Fatal("expanded an impossible group", got)
	}
}

func TestWeakestEventAssignmentAndDistinctAircraftIdentity(t *testing.T) {
	c := DefaultConfig()
	c.ScoreObjective = "weakest"
	choices := [][]Match{{{Track: "A", Score: 20}, {Track: "B", Score: 10}}, {{Track: "B", Score: 4}, {Track: "A", Score: 9}}}
	got := configuredMatches(c, choices, 0)
	if len(got) != 2 || got[0].Track != "B" || got[1].Track != "A" {
		t.Fatal("bright event concealed weak event", got)
	}
	c.DistinctAircraft = true
	choices = [][]Match{{{Track: "abc123-1700000000.000-1", Score: 20}}, {{Track: "abc123-1700000000.000-2", Score: 20}}}
	if len(configuredMatches(c, choices, 0)) != 0 {
		t.Fatal("one aircraft's legs counted as independent airplanes")
	}
}

func TestReservedPixelsCannotChangeTrainingPositionClockOrSourceTone(t *testing.T) {
	c, s, ts, _ := automaticSequenceFixture()
	c = subsetSequenceConfig(c, []int{0, 1})
	a, err := Run(context.Background(), c, s, ts)
	if err != nil || len(a.Candidates) == 0 {
		t.Fatal("missing training fixture", err)
	}
	changed := make([]*acoustic.Spectrum, len(c.Events))
	for i, e := range c.Events {
		v := *s
		v.Rows = make([][]float32, len(s.Rows))
		plan := eventFold(e, s)
		for k, row := range s.Rows {
			v.Rows[k] = append([]float32(nil), row...)
			if plan.Contains(s.Time(k), "validation") {
				for j := range row {
					v.Rows[k][j] = 30
				}
			}
		}
		changed[i] = &v
	}
	c.eventSpectra = changed
	b, err := Run(context.Background(), c, s, ts)
	if err != nil || len(b.Candidates) == 0 {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a.Candidates, b.Candidates) {
		t.Fatal("reserved spectral values changed fitted parameters")
	}
}

func TestTrainingInterpolationDoesNotReadReservedRidgeSamples(t *testing.T) {
	e := acoustic.Event{Start: 0, End: 40, Resolution: 2}
	s := &acoustic.Spectrum{SampleRate: 1024, Window: 2048, Hop: 512}
	p := eventFold(e, s)
	for at := 0.; at <= 40; at += .5 {
		e.Ridge = append(e.Ridge, acoustic.RidgeSample{Time: at, Hz: 130 - at})
	}
	before := prepareFoldObservations(s, e, "train")
	// Populate rows before exercising the full observation builder.
	for k := 0; k < 80; k++ {
		s.Rows = append(s.Rows, make([]float32, 200))
	}
	before = prepareFoldObservations(s, e, "train")
	changed := e
	changed.Ridge = append([]acoustic.RidgeSample(nil), e.Ridge...)
	for i := range changed.Ridge {
		if !p.Contains(changed.Ridge[i].Time, "train") {
			changed.Ridge[i].Hz += 1000
		}
	}
	after := prepareFoldObservations(s, changed, "train")
	if !reflect.DeepEqual(before, after) || len(before) == 0 {
		t.Fatal("reserved ridge values leaked into interpolation")
	}
}
