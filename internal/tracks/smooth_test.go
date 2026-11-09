package tracks

import (
	"math"
	"testing"

	"pinpoint/internal/geo"
)

func TestSmoothedVelocityAgainstIndependentAcceleratingSource(t *testing.T) {
	pos := func(t float64) geo.Vec { return geo.Vec{X: 90*t + .05*t*t, Y: 1500 + .02*t*t, Z: 800} }
	velocity := func(t float64) geo.Vec { return geo.Vec{X: 90 + .1*t, Y: .04 * t} }
	tr := Track{ID: "accelerating"}
	for t := -60.; t <= 60; t += 10 {
		tr.Points = append(tr.Points, Point{Time: t, Position: pos(t)})
	}
	smooth, r, err := tr.Smooth(SmoothingOptions{Step: .1, MaxGap: 15, MaxAcceleration: 5})
	if err != nil || r.Rejected != 0 || r.Gaps != 0 || r.Smoothed != 12 {
		t.Fatal("failed smooth trajectory", r, err)
	}
	originalError, smoothError := 0., 0.
	for te := -35.; te <= 35; te += .37 {
		p := pos(te)
		d := p.Norm()
		heard := te + d/343
		want := 343 / (343 + p.Dot(velocity(te))/d)
		a, _, ok := tr.Received(geo.Vec{}, heard, 343, 15)
		b, _, yes := smooth.Received(geo.Vec{}, heard, 343, 15)
		if !ok || !yes {
			t.Fatal("lost physical coverage")
		}
		originalError += math.Pow(a-want, 2)
		smoothError += math.Pow(b-want, 2)
	}
	if smoothError >= originalError*.001 {
		t.Fatalf("smoothing did not improve velocity on known quadratic motion: %g / %g", smoothError, originalError)
	}
	for _, p := range tr.Points {
		found := false
		for _, q := range smooth.Points {
			if q.Time == p.Time {
				found = q.Position == p.Position
				break
			}
		}
		if !found {
			t.Fatal("changed measured telemetry node")
		}
	}
	if tr.receiver != nil {
		t.Fatal("mutated original track/cache")
	}
}

func TestSmoothingPreservesStraightFlightAndTelemetryGaps(t *testing.T) {
	tr := Track{Points: []Point{{Time: -10, Position: geo.Vec{X: -900, Y: 1500}}, {Time: 0, Position: geo.Vec{Y: 1500}}, {Time: 100, Position: geo.Vec{X: 9000, Y: 1500}}, {Time: 110, Position: geo.Vec{X: 9900, Y: 1500}}}}
	s, r, err := tr.Smooth(SmoothingOptions{Step: .25, MaxGap: 15, MaxAcceleration: 5})
	if err != nil || r.Gaps != 1 || r.MaxDeviation > 1e-8 {
		t.Fatal("changed straight flight/gap", r, err)
	}
	for _, te := range []float64{-5, 105} {
		p := geo.Vec{X: 90 * te, Y: 1500}
		heard := te + p.Norm()/343
		a, d, ok := tr.Received(geo.Vec{}, heard, 343, 15)
		b, e, yes := s.Received(geo.Vec{}, heard, 343, 15)
		if !ok || !yes || math.Abs(a-b) > 1e-9 || math.Abs(d-e) > 1e-6 {
			t.Fatal("changed constant-velocity sound")
		}
	}
	if _, _, ok := s.Received(geo.Vec{}, 50, 343, 15); ok {
		t.Fatal("smoothing invented missing telemetry")
	}
	if _, _, err = tr.Smooth(SmoothingOptions{Step: math.NaN(), MaxGap: 15, MaxAcceleration: 5}); err == nil {
		t.Fatal("accepted invalid smoothing")
	}
}
