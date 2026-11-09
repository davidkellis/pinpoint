package search

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"testing"
	"time"

	"pinpoint/internal/acoustic"
	"pinpoint/internal/geo"
	"pinpoint/internal/tracks"
)

const consensusEpoch = 1700000000.

// straightFlight returns a constant-velocity track passing the local offset
// (cpaEast, cpaNorth) metres from (lat, lon) at height `alt` at emission time
// cpaTime (seconds after consensusEpoch), heading along unit (east, north).
func straightFlight(id string, lat, lon, cpaEast, cpaNorth, alt, cpaTime, east, north, speed float64) tracks.Track {
	t := tracks.Track{ID: id}
	for dt := -400.; dt <= 400; dt += 2 {
		e, n := cpaEast+east*speed*dt, cpaNorth+north*speed*dt
		la := lat + n/111320
		lo := lon + e/(111320*math.Cos(lat*math.Pi/180))
		t.Points = append(t.Points, tracks.Point{Time: consensusEpoch + cpaTime + dt, Lat: la, Lon: lo, Position: geo.ECEF(la, lo, alt)})
	}
	return t
}

// heardCurve synthesizes the ridge a stationary receiver records from a track
// emitting a constant tone f0, with the recording clock running `clock` seconds
// behind true time (so audio time = true arrival - epoch - clock).
func heardCurve(obs geo.Vec, t tracks.Track, f0, start, end, clock float64) acoustic.Event {
	const c = 343.
	pos := func(te float64) geo.Vec {
		for i := 1; i < len(t.Points); i++ {
			a, b := t.Points[i-1], t.Points[i]
			if te >= a.Time && te <= b.Time {
				u := (te - a.Time) / (b.Time - a.Time)
				return a.Position.Add(b.Position.Sub(a.Position).Scale(u))
			}
		}
		return t.Points[len(t.Points)-1].Position
	}
	arrival := func(te float64) float64 { return te + pos(te).Sub(obs).Norm()/c }
	e := acoustic.Event{Start: start, End: end, Resolution: 1}
	for te := t.Points[0].Time + 1; te < t.Points[len(t.Points)-1].Time-1; te += .5 {
		heard := arrival(te) - consensusEpoch - clock
		if heard < start || heard > end {
			continue
		}
		const dt = 1e-3
		factor := 2 * dt / (arrival(te+dt) - arrival(te-dt))
		e.Ridge = append(e.Ridge, acoustic.RidgeSample{Time: heard, Hz: f0 * factor})
	}
	return e
}

func consensusScene(clock float64) (lat, lon float64, ts []tracks.Track, events []acoustic.Event) {
	lat, lon = 30.01, -97.99
	obs := geo.ECEF(lat, lon, 200)
	// Two distinct aircraft pass the receiver 300 s apart on different headings.
	a := straightFlight("aaaaaa-1-1", lat, lon, 0, 900, 1400, 100, 1, 0, 70)
	b := straightFlight("bbbbbb-1-1", lat, lon, -1500, 0, 2000, 400, 0, 1, 110)
	// A decoy aircraft is in the area but explains nothing.
	d := straightFlight("dddddd-1-1", lat, lon, 4000, 0, 2700, 250, 0, -1, 120)
	e1 := heardCurve(obs, a, 110, 60, 150, clock)
	e2 := heardCurve(obs, b, 160, 370, 450, clock)
	// A sound with no aircraft explanation, and a harmonic that overlaps e1.
	junk := acoustic.Event{Start: 200, End: 260, Resolution: 1}
	for s := 200.; s <= 260; s += .5 {
		junk.Ridge = append(junk.Ridge, acoustic.RidgeSample{Time: s, Hz: 300 + 40*math.Sin(s/7)})
	}
	harmonic := e1
	harmonic.Ridge = append([]acoustic.RidgeSample(nil), e1.Ridge...)
	for i := range harmonic.Ridge {
		harmonic.Ridge[i].Hz *= 2
	}
	return lat, lon, []tracks.Track{a, b, d}, []acoustic.Event{e1, harmonic, junk, e2}
}

func consensusReceiver(lat, lon, clock float64) CurveReceiver {
	return CurveReceiver{ReceiverHypothesis: ReceiverHypothesis{Lat: lat, Lon: lon, Height: 200, RecordingStart: time.Unix(int64(consensusEpoch), 0).UTC().Format(time.RFC3339), Clock: clock, SoundSpeed: 343, MaxGap: 30}}
}

func TestRidgeSubsetLeavesUnexplainedAndOverlappingCurvesOut(t *testing.T) {
	lat, lon, ts, events := consensusScene(0)
	o := CurveModelOptions{Window: 1}
	for i := range ts {
		ts[i] = ts[i].PrepareSegments()
	}
	all, err := FitJointRidges(context.Background(), consensusReceiver(lat, lon, 0), ts, []acoustic.Event{events[0], events[2], events[3]}, o)
	if err != nil {
		t.Fatal(err)
	}
	if all.Valid && all.WorstTraining < .5 {
		t.Fatal("requiring every curve should be spoiled by the unexplained sound", all)
	}
	r, err := FitRidgeSubset(context.Background(), consensusReceiver(lat, lon, 0), ts, events, o, 2, DefaultSubsetCriteria())
	if err != nil || !r.Valid || r.Worst > .01 || r.Explained != 2 {
		t.Fatal("lost the two explained curves", r, err)
	}
	got := map[int]string{}
	for _, m := range r.Matches {
		got[m.Event] = m.Aircraft
	}
	if got[1] != "aaaaaa" || got[4] != "bbbbbb" || len(got) != 2 {
		t.Fatal("wrong curves or aircraft in subset", got)
	}
	// With only one explainable aircraft no two-curve consensus exists.
	r, err = FitRidgeSubset(context.Background(), consensusReceiver(lat, lon, 0), []tracks.Track{ts[0], ts[2]}, events, o, 2, DefaultSubsetCriteria())
	if err != nil || r.Valid {
		t.Fatal("one aircraft cannot provide two-curve consensus", r, err)
	}
	// A wrong shared clock spoils both curves together.
	r, err = FitRidgeSubset(context.Background(), consensusReceiver(lat, lon, 8), ts, events, o, 2, DefaultSubsetCriteria())
	if err != nil || r.Valid && r.Worst < .05 {
		t.Fatal("wrong common clock still fit", r, err)
	}
	if _, err = FitRidgeSubset(context.Background(), consensusReceiver(lat, lon, 0), ts, events, o, 5, DefaultSubsetCriteria()); err == nil {
		t.Fatal("accepted more required curves than supplied")
	}
}

func TestConsensusRecoversReceiverAndClockFromTwoCurves(t *testing.T) {
	const clock = 3.
	lat, lon, ts, events := consensusScene(clock)
	o := DefaultConsensusOptions()
	o.Bounds = geo.Bounds{South: lat - .03, North: lat + .03, West: lon - .035, East: lon + .035}
	o.RecordingStart = time.Unix(int64(consensusEpoch), 0).UTC().Format(time.RFC3339)
	o.Grid, o.Refine, o.Final = 600, 100, 20
	o.RefinePeaks, o.FinalPeaks = 6, 3
	o.ClockWindow, o.ClockStep, o.FineClockStep = 6, 3, 1
	o.Window = 1
	o.Height = func(float64, float64) (float64, error) { return 200, nil }
	r, err := Consensus(context.Background(), events, ts, o)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Regions) == 0 {
		t.Fatal("no consensus region", r.Status)
	}
	best := r.Regions[0]
	miss := geo.ECEF(best.Lat, best.Lon, 0).Sub(geo.ECEF(lat, lon, 0)).Norm()
	if miss > 60 || math.Abs(best.Clock-clock) > .51 || best.Explains != 2 {
		t.Fatalf("consensus missed: %.1f m, clock %.2f, explains %d", miss, best.Clock, best.Explains)
	}
	if _, err := json.Marshal(r); err != nil {
		t.Fatal("result must be JSON encodable", err)
	}
}

func TestConsensusStatusUsesSeparatedRunnerUp(t *testing.T) {
	p := func(lat, worst float64) ConsensusPoint { return ConsensusPoint{Lat: lat, Worst: worst, Valid: true} }
	regions := ConsensusRegions([]ConsensusPoint{p(30, .2), p(30.001, .21), p(31, .25)}, 3218.688, 10)
	if len(regions) != 2 || regions[1].Lat != 31 {
		t.Fatal("nearby point should merge into the winner's region", regions)
	}
	s, m, radius := consensusStatus(regions, 1.5)
	if s != "ambiguous" || m == nil || math.Abs(*m-1.25) > 1e-9 || radius < 100000 {
		t.Fatal("near-tied separated regions must be ambiguous with a wide agreement radius", s, m, radius)
	}
	if s, _, radius := consensusStatus(ConsensusRegions([]ConsensusPoint{p(30, .2), p(31, .5)}, 3218.688, 10), 1.5); s != "resolved" || radius != 0 {
		t.Fatal("clearly separated winner should resolve", s, radius)
	}
	if s, m, _ := consensusStatus(nil, 1.5); s != "no_match" || m != nil {
		t.Fatal("no regions means no match")
	}
	// A loudness-contradicted runner-up cannot make the winner ambiguous, and a
	// contradicted winner is never resolved.
	q := p(31, .21)
	q.Contradicted = true
	if s, _, _ := consensusStatus([]ConsensusPoint{p(30, .2), q}, 1.5); s != "resolved" {
		t.Fatal("contradicted runner-up must not cause ambiguity", s)
	}
	if s, _, _ := consensusStatus([]ConsensusPoint{q, p(30, .5)}, 1.5); s != "contradicted" {
		t.Fatal("contradicted winner must be reported", s)
	}
}

func TestLoudnessCheckRewardsHeardClosePassesAndFlagsQuietOnes(t *testing.T) {
	lat, lon := 30.01, -97.99
	obs := geo.ECEF(lat, lon, 200)
	// Six passes at increasing distance, arriving 300 s apart.
	var ts []tracks.Track
	for k := 0; k < 6; k++ {
		ts = append(ts, straightFlight(fmt.Sprintf("p%05d-1-1", k), lat, lon, 0, 500+float64(k)*300, 900, 300+300*float64(k), 1, 0, 80))
	}
	level := make([]float64, 2400)
	for i := range level {
		level[i] = -40
	}
	loud := acoustic.Loudness{Step: 1, Level: level}
	heard := acoustic.Loudness{Step: 1, Level: append([]float64(nil), level...)}
	o := DefaultLoudnessOptions()
	for _, tr := range ts {
		for _, pass := range closePasses(tr, obs, consensusEpoch, o) {
			gain := 30 - 20*math.Log10(pass.Slant/500)
			for i := int(pass.Arrival) - 8; i <= int(pass.Arrival)+8; i++ {
				heard.Level[i] += gain
			}
		}
	}
	c := CheckLoudness(heard, ts, obs, consensusEpoch, o)
	if c.Passes != 6 || c.Spearman < .99 || c.Violations != 0 || c.Contradicts() {
		t.Fatal("heard, distance-ordered passes should agree", c)
	}
	q := CheckLoudness(loud, ts, obs, consensusEpoch, o)
	if q.Passes != 6 || q.Violations < 2 || !q.Contradicts() {
		t.Fatal("silent audio under close passes should contradict", q)
	}
}

func TestSubsetCriteriaRejectUninformativeCurves(t *testing.T) {
	c := DefaultSubsetCriteria()
	q := func(train, linear, constant float64) JointRidgeMatch {
		return JointRidgeMatch{NormalizedTraining: train / 1.5, Fit: CurveModelFit{Training: train, TrainLinear: linear, TrainConstant: constant}}
	}
	if !c.eligible(q(.75, 5.8, 16)) {
		t.Fatal("a well-explained, strongly bent curve must count")
	}
	if c.eligible(q(.5, 1.1, 1.47)) {
		t.Fatal("a nearly flat curve fits any aircraft and must not count")
	}
	if c.eligible(q(3.7, 7.2, 8.8)) {
		t.Fatal("a fit barely better than a straight line must not count")
	}
}
