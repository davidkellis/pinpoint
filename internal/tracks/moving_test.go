package tracks

import (
	"math"
	"testing"

	"pinpoint/internal/geo"
)

// Independently emit wavefronts and solve their intersection with a linear
// receiver. Differentiated arrival spacings verify frequency, including the
// numerator due to receiver velocity and the retarded source position.
func TestMovingReceiverAgainstForwardWavefrontSpacing(t *testing.T) {
	const c = 343.
	sourceOrigin := geo.Vec{Y: 1300, Z: 800}
	sourceVelocity := geo.Vec{X: 95, Y: 12, Z: 3}
	tr := Track{ID: "source", Points: []Point{{Time: -300, Position: sourceOrigin.Add(sourceVelocity.Scale(-300))}, {Time: 300, Position: sourceOrigin.Add(sourceVelocity.Scale(300))}}}
	for _, rv := range []geo.Vec{{}, {X: 3, Y: -1}, {X: -4, Z: .3}} {
		arrival := func(te float64) float64 {
			q := sourceOrigin.Add(sourceVelocity.Sub(rv).Scale(te))
			b := q.Dot(rv)
			a := c*c - rv.Dot(rv)
			lag := q.Dot(q) / (math.Sqrt(b*b+a*q.Dot(q)) + b)
			return te + lag
		}
		for _, te := range []float64{-70, -12, 0, 15, 60} {
			heard := arrival(te)
			factor, d, emitted, ok := tr.ReceivedMoving(rv.Scale(heard), rv, heard, c, 1000)
			const dt = 1e-3
			want := 2 * dt / (arrival(te+dt) - arrival(te-dt))
			if !ok || math.Abs(factor-want) > 1e-8 || math.Abs(emitted-te) > 1e-7 || math.Abs(d-c*(heard-te)) > 1e-5 {
				t.Fatalf("velocity %+v emission %g: %g %g %g %v; want %g", rv, te, factor, d, emitted, ok, want)
			}
			if rv == (geo.Vec{}) {
				fixed, dist, yes := tr.Received(geo.Vec{}, heard, c, 1000)
				if !yes || fixed != factor || dist != d {
					t.Fatal("zero motion changed fixed receiver results")
				}
			}
		}
	}
}

func TestMovingReceiverDoesNotBridgeGapsOrAcceptInvalidVelocity(t *testing.T) {
	tr := Track{Points: []Point{{Time: -100, Position: geo.Vec{X: -9000, Y: 1500}}, {Time: 100, Position: geo.Vec{X: 9000, Y: 1500}}}}
	for _, v := range []geo.Vec{{X: 343}, {Y: math.NaN()}} {
		if _, _, _, ok := tr.ReceivedMoving(geo.Vec{}, v, 0, 343, 300); ok {
			t.Fatal("invalid motion accepted")
		}
	}
	if _, _, _, ok := tr.ReceivedMoving(geo.Vec{}, geo.Vec{X: 2}, 0, 343, 30); ok {
		t.Fatal("motion bridged telemetry gap")
	}
}
