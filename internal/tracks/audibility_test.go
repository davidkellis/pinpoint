package tracks

import (
	"math"
	"pinpoint/internal/geo"
	"testing"
)

func TestAudibilityIntervalsCoverEntirePassAndDoNotBridgeGaps(t *testing.T) {
	tr := Track{Points: []Point{{Time: 0, Position: geo.Vec{X: -1000, Y: 300}}, {Time: 20, Position: geo.Vec{X: 1000, Y: 300}}}}
	w := tr.WithinRadius(geo.Vec{}, 500, 343, 30)
	// Sphere intersections occur at x=+-400 (emission 6 and 14 seconds).
	if len(w) != 1 || math.Abs(w[0].Start-(6+500.0/343)) > 1e-8 || math.Abs(w[0].End-(14+500.0/343)) > 1e-8 {
		t.Fatalf("incorrect arrival window: %+v", w)
	}
	if len(tr.WithinRadius(geo.Vec{}, 200, 343, 30)) > 0 {
		t.Fatal("out-of-range aircraft considered audible")
	}
	if len(tr.WithinRadius(geo.Vec{}, 500, 343, 10)) > 0 {
		t.Fatal("interpolated through telemetry gap")
	}
}
