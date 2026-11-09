package tracks

import (
	"math"
	"math/rand"
	"pinpoint/internal/geo"
	"testing"
)

func TestDopplerEnvelopeContainsMovingGeometryAndObserverBall(t *testing.T) {
	const epoch = 1700000000.0
	tr := Track{ID: "turning-climbing"}
	for sec := -150.; sec <= 150; sec += 3 {
		p := geo.Vec{X: 120 * sec, Y: 1500, Z: 800}
		if sec > 0 {
			p = geo.Vec{X: 80 * sec, Y: 1500 + 90*sec, Z: 800 + 3*sec}
		}
		tr.Points = append(tr.Points, Point{Time: epoch + sec, Position: p})
	}
	rng := rand.New(rand.NewSource(16))
	for _, radius := range []float64{0, 100, 400, 2000} {
		for _, at := range []float64{-60, -10, 0, 10, 40} {
			lo, hi, ok := tr.DopplerBounds(geo.Vec{}, radius, epoch+at-2, epoch+at+2, 343, 10)
			if !ok {
				t.Fatal("missing envelope")
			}
			for n := 0; n < 400; n++ {
				p := geo.Vec{X: rng.NormFloat64(), Y: rng.NormFloat64(), Z: rng.NormFloat64()}
				if radius == 0 {
					p = geo.Vec{}
				} else {
					p = p.Scale(radius * math.Cbrt(rng.Float64()) / p.Norm())
				}
				f, _, valid := tr.Received(p, epoch+at+4*rng.Float64()-2, 343, 10)
				if valid && (f < lo-1e-8 || f > hi+1e-8) {
					t.Fatalf("gain %.10f escaped [%.10f,%.10f], radius %g", f, lo, hi, radius)
				}
			}
		}
	}
	if _, _, ok := tr.DopplerBounds(geo.Vec{}, 100, epoch+1, epoch+2, 343, 1); ok {
		t.Fatal("bridged telemetry gaps")
	}
}
