package search

import (
	"context"
	"pinpoint/internal/acoustic"
	"pinpoint/internal/geo"
	"testing"
)

func TestMeasuredJointSearchStartsFromWholeRegion(t *testing.T) {
	c, s, ts, truth, _ := timingFixture()
	c.Staged = false
	c.Grid = 2000
	c.DenseGrid = 0
	c.Refine = 150
	for i := range c.Events {
		c.Events[i].Evidence = &acoustic.CurveEvidence{Level: "strong", Predictive: true}
		c.Events[i].Measurement = &acoustic.RidgeMeasurement{ShapePass: true}
	}
	p, err := MeasuredJointConfig(c, c.Events)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Staged || p.Grid != 200 || p.DenseGrid != 100 || p.Refine != 100 || p.Bounds != c.Bounds || p.ClockUncertainty != c.ClockUncertainty || len(p.Events) != 2 {
		t.Fatal("lost whole-area joint configuration")
	}
	r, err := Run(context.Background(), p, s, ts)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Candidates) == 0 || r.GeometrySurvived == 0 || r.FinalGrid != 10 {
		t.Fatalf("joint search did not preserve/refine survivors: %+v", r)
	}
	best := r.Candidates[0]
	if geo.ECEF(best.Lat, best.Lon, best.Height).Sub(truth.pos).Norm() > 100 {
		t.Fatal("discarded the joint location")
	}
	for i := range ts[1].Points {
		ts[1].Points[i].Time += 20
	}
	if _, _, ok := evaluatePoint(context.Background(), truth, p, s, ts, 1700000000); ok {
		t.Fatal("accepted changed relative flyover timing")
	}
	if _, err = MeasuredJointConfig(c, c.Events[:1]); err == nil {
		t.Fatal("silently omitted another reviewed flyover")
	}
}
