package search

import (
	"context"
	"math"
	"reflect"
	"testing"
	"time"

	"pinpoint/internal/acoustic"
	"pinpoint/internal/geo"
	"pinpoint/internal/tracks"
)

func TestCurveModelCanSkipPlotSamplesWithoutChangingAnyFitStatistic(t *testing.T) {
	h, tr, e, o := changingPitchFixture()
	a, err := FitRidgeCurve(context.Background(), h, tr, e, o)
	if err != nil {
		t.Fatal(err)
	}
	o.OmitCurve = true
	b, err := FitRidgeCurve(context.Background(), h, tr, e, o)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Fits) != len(b.Fits) {
		t.Fatal("changed model count")
	}
	for i := range a.Fits {
		if len(a.Fits[i].Curve) == 0 || len(b.Fits[i].Curve) != 0 {
			t.Fatal("plot work was not omitted")
		}
		a.Fits[i].Curve = nil
		b.Fits[i].Curve = nil
		if !reflect.DeepEqual(a.Fits[i], b.Fits[i]) {
			t.Fatal("skipping plots changed model scoring", a.Fits[i], b.Fits[i])
		}
	}
}

func changingPitchFixture() (CurveReceiver, tracks.Track, acoustic.Event, CurveModelOptions) {
	const c = 343.
	const epoch = 1700000000.
	h := CurveReceiver{ReceiverHypothesis: ReceiverHypothesis{RecordingStart: time.Unix(int64(epoch), 0).UTC().Format(time.RFC3339), SoundSpeed: c, MaxGap: 30}, East: 2, North: 1}
	obs := geo.ECEF(0, 0, 0)
	velocity := geo.Vec{Y: 2, Z: 1}
	sourceVelocity := geo.Vec{Y: 85}
	sourceOrigin := geo.Vec{X: 1500, Y: -85 * 40, Z: 800}
	arrival := func(te float64) float64 {
		q := sourceOrigin.Add(sourceVelocity.Sub(velocity).Scale(te))
		b := q.Dot(velocity)
		a := c*c - velocity.Dot(velocity)
		return te + q.Dot(q)/(math.Sqrt(b*b+a*q.Dot(q))+b)
	}
	e := acoustic.Event{Start: 0, End: 100, Resolution: 1}
	for te := -10.; te < 110; te += .5 {
		heard := arrival(te)
		if heard < e.Start || heard > e.End {
			continue
		}
		const dt = 1e-3
		factor := 2 * dt / (arrival(te+dt) - arrival(te-dt))
		e.Ridge = append(e.Ridge, acoustic.RidgeSample{Time: heard, Hz: (1500 + 1.5*(te-40)) * factor})
	}
	t := tracks.Track{ID: "synthetic-changing-pitch"}
	for te := -100.; te <= 300; te += 2 {
		pos := obs.Add(sourceOrigin).Add(sourceVelocity.Scale(te))
		t.Points = append(t.Points, tracks.Point{Time: epoch + te, Position: pos})
	}
	return h, t, e, CurveModelOptions{Window: 1, MaxSourceChange: .15}
}

func selectedModel(t *testing.T, r CurveModelReview) CurveModelFit {
	t.Helper()
	for _, f := range r.Fits {
		if f.Selected {
			return f
		}
	}
	t.Fatalf("no selected model: %+v", r)
	return CurveModelFit{}
}

func TestCurveModelRecoversChangingEmissionPitchAndMovingReceiver(t *testing.T) {
	h, tr, e, o := changingPitchFixture()
	r, err := FitRidgeCurve(context.Background(), h, tr, e, o)
	if err != nil {
		t.Fatal(err)
	}
	f := selectedModel(t, r)
	if f.Model != "linear_source" || math.Abs(f.Slope-1.5) > .001 || f.Reserved > .001 || !f.Shape || f.AtBound {
		t.Fatalf("failed physical source/motion model: %+v", f)
	}
	h.East, h.North = 0, 0
	fixed, err := FitRidgeCurve(context.Background(), h, tr, e, o)
	if err != nil {
		t.Fatal(err)
	}
	if selectedModel(t, fixed).Reserved < .5 {
		t.Fatal("stationary proxy incorrectly matched moving microphone exactly")
	}
}

func TestCurveModelBoundsPitchAndDoesNotUseReservedMeasurementsForSelection(t *testing.T) {
	h, tr, e, o := changingPitchFixture()
	o.MaxSourceChange = .02
	r, err := FitRidgeCurve(context.Background(), h, tr, e, o)
	if err != nil {
		t.Fatal(err)
	}
	chosen := selectedModel(t, r)
	if !chosen.AtBound || math.Abs(chosen.Change) > .020001 || chosen.Reserved < 1 {
		t.Fatalf("ignored source variation bound: %+v", chosen)
	}
	for i := range e.Ridge {
		if r.Fold.Contains(e.Ridge[i].Time, "validation") {
			e.Ridge[i].Hz += 300 * math.Sin(e.Ridge[i].Time)
		}
	}
	mutated, err := FitRidgeCurve(context.Background(), h, tr, e, o)
	if err != nil {
		t.Fatal(err)
	}
	f := selectedModel(t, mutated)
	if f.Source != chosen.Source || f.Slope != chosen.Slope || f.Criterion != chosen.Criterion || f.Model != chosen.Model {
		t.Fatal("reserved ridge data influenced model selection")
	}
}

func TestCurveModelRejectsLinearRidgeAndInvalidInput(t *testing.T) {
	h, tr, e, o := changingPitchFixture()
	for i := range e.Ridge {
		e.Ridge[i].Hz = 1800 - 8*e.Ridge[i].Time
	}
	r, err := FitRidgeCurve(context.Background(), h, tr, e, o)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range r.Fits {
		if f.Shape {
			t.Fatal("straight chirp declared a distinguishing flight shape")
		}
	}
	h.East = math.NaN()
	if _, err = FitRidgeCurve(context.Background(), h, tr, e, o); err == nil {
		t.Fatal("accepted nonfinite motion")
	}
	h.East = 2
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = FitRidgeCurve(ctx, h, tr, e, o); err != context.Canceled {
		t.Fatal("ignored cancellation")
	}
}
