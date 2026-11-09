package tracks

import (
	"math"
	"reflect"
	"testing"

	"pinpoint/internal/geo"
)

// Ground truth is analytic motion, not the interpolation or filtering code.
// Velocity reports and quantized/noisy position reports are generated separately.
func noisyTelemetry(speed float64) (Track, func(float64) geo.Vec, func(float64) geo.Vec, geo.Vec) {
	origin := geo.ECEF(37, -121, 0)
	position := func(t float64) geo.Vec {
		return origin.Add(geo.ENUVector(37, -121, speed*t+.025*t*t, 1400+20*math.Sin(t/25)+.01*t*t, 1100+2*t+30*math.Sin(t/35)))
	}
	velocity := func(t float64) geo.Vec {
		return geo.ENUVector(37, -121, speed+.05*t, .8*math.Cos(t/25)+.02*t, 2+30./35*math.Cos(t/35))
	}
	tr := Track{ID: "analytic-aircraft", TypeCode: "synthetic"}
	for i, tm := 0, -100.; tm <= 160; i, tm = i+1, tm+5 {
		p := position(tm)
		lat, lon, alt := geo.Geodetic(p)
		noisy := geo.ECEF(lat, lon, math.Round(alt/7.62)*7.62).Add(geo.ENUVector(lat, lon, 6*math.Sin(float64(i)*2.3), 5*math.Cos(float64(i)*3.5), 3*math.Sin(float64(i)*1.7)))
		la, lo, al := geo.Geodetic(noisy)
		tr.Points = append(tr.Points, Point{Time: tm, Lat: la, Lon: lo, Alt: al, Position: noisy})
		v := velocity(tm)
		e, n, u := v.Dot(geo.ENUVector(la, lo, 1, 0, 0)), v.Dot(geo.ENUVector(la, lo, 0, 1, 0)), v.Dot(geo.ENUVector(la, lo, 0, 0, 1))
		e += .25 * math.Sin(float64(i)*1.3)
		n += .3 * math.Cos(float64(i)*2.1)
		u += .15 * math.Sin(float64(i)*2.8)
		tr.Motion = append(tr.Motion, KinematicPoint{Time: tm, GroundSpeed: math.Hypot(e, n), TrueTrack: math.Mod(math.Atan2(e, n)*180/math.Pi+360, 360), Up: u, Horizontal: true, Vertical: true})
	}
	return tr, position, velocity, origin
}

func TestReconstructionReducesDopplerErrorForQuantizedClimbingPropAndJet(t *testing.T) {
	for _, tc := range []struct{ speed, frequency float64 }{{95, 130}, {185, 1500}} {
		tr, position, velocity, observer := noisyTelemetry(tc.speed)
		originalPoints := append([]Point(nil), tr.Points...)
		originalMotion := append([]KinematicPoint(nil), tr.Motion...)
		smoothing := SmoothingOptions{Step: .1, MaxGap: 30, MaxAcceleration: 20, UseKinematics: true}
		before, _, err := tr.Smooth(smoothing)
		if err != nil {
			t.Fatal(err)
		}
		reconstructed, report, err := tr.Reconstruct(DefaultReconstructionOptions())
		if err != nil || report.Rejected != 0 || report.Horizontal != len(tr.Points) || report.Vertical != len(tr.Points) || report.MaxShift > 30 {
			t.Fatal("unexpected reconstruction", report, err)
		}
		after, _, err := reconstructed.Smooth(smoothing)
		if err != nil {
			t.Fatal(err)
		}
		squaredBefore, squaredAfter, count := 0., 0., 0
		for te := -40.; te <= 80; te += .37 {
			rangeVector := position(te).Sub(observer)
			distance := rangeVector.Norm()
			arrival := te + distance/343
			want := 343 / (343 + rangeVector.Dot(velocity(te))/distance)
			v0, _, ok0 := before.Received(observer, arrival, 343, 30)
			v1, _, ok1 := after.Received(observer, arrival, 343, 30)
			if !ok0 || !ok1 {
				t.Fatal("lost observed coverage")
			}
			squaredBefore += math.Pow(tc.frequency*(v0-want), 2)
			squaredAfter += math.Pow(tc.frequency*(v1-want), 2)
			count++
		}
		rmse0, rmse1 := math.Sqrt(squaredBefore/float64(count)), math.Sqrt(squaredAfter/float64(count))
		t.Logf("%.0f Hz tone: measured-node interpolation %.4f Hz, reconstructed %.4f Hz; max node correction %.2f m", tc.frequency, rmse0, rmse1, report.MaxShift)
		if rmse1 >= rmse0*.5 || rmse1 > tc.frequency*.001 {
			t.Fatal("failed to reduce independent analytic Doppler error", rmse0, rmse1)
		}
		if !reflect.DeepEqual(originalPoints, tr.Points) || !reflect.DeepEqual(originalMotion, tr.Motion) || tr.receiver != nil {
			t.Fatal("mutated original observations/cache")
		}
	}
}

func TestReconstructionNeverLearnsAcrossMissingTelemetry(t *testing.T) {
	tr, _, _, _ := noisyTelemetry(100)
	tr.Points = append(append([]Point(nil), tr.Points[:12]...), tr.Points[35:]...)
	opts := DefaultReconstructionOptions()
	full, report, err := tr.Reconstruct(opts)
	if err != nil || report.Chunks != 2 || report.Rejected != 0 {
		t.Fatal(report, err)
	}
	for _, bounds := range [][2]int{{0, 12}, {12, len(tr.Points)}} {
		part := tr
		part.Points = part.Points[bounds[0]:bounds[1]]
		separate, _, err := part.Reconstruct(opts)
		if err != nil || !reflect.DeepEqual(separate.Points, full.Points[bounds[0]:bounds[1]]) {
			t.Fatal("filter/smoother leaked across a gap", err)
		}
	}
	smooth, _, err := full.Smooth(SmoothingOptions{Step: .25, MaxGap: 30, MaxAcceleration: 20, UseKinematics: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, ok := smooth.Received(geo.ECEF(37, -121, 0), 0, 343, 30); ok {
		t.Fatal("invented sound across missing telemetry")
	}
	opts.VerticalVelocity = math.NaN()
	if _, _, err := tr.Reconstruct(opts); err == nil {
		t.Fatal("accepted nonfinite uncertainty")
	}
}

func TestReconstructionDisclosesAndRejectsExcessiveNodeCorrections(t *testing.T) {
	tr, _, _, _ := noisyTelemetry(100)
	opts := DefaultReconstructionOptions()
	opts.MaxDisplacement = .001
	out, report, err := tr.Reconstruct(opts)
	if err != nil || report.Rejected != 1 || !reflect.DeepEqual(out.Points, tr.Points) || !reflect.DeepEqual(out.Motion, tr.Motion) {
		t.Fatal("changed observations after displacement rejection", report, err)
	}
}

func TestReconstructedNorthBearingSurvivesMotionCSVPrecision(t *testing.T) {
	origin := geo.ECEF(0, 0, 1000)
	tr := Track{ID: "due-north"}
	for i := 0.; i < 12; i++ {
		p := origin.Add(geo.ENUVector(0, 0, -1e-12*i, 100*i, 0))
		lat, lon, alt := geo.Geodetic(p)
		tr.Points = append(tr.Points, Point{Time: i, Lat: lat, Lon: lon, Alt: alt, Position: p})
		tr.Motion = append(tr.Motion, KinematicPoint{Time: i, GroundSpeed: 100, TrueTrack: 0, Horizontal: true, Vertical: true})
	}
	out, r, err := tr.Reconstruct(DefaultReconstructionOptions())
	if err != nil || r.Rejected != 0 {
		t.Fatal(r, err)
	}
	if _, _, err = out.Smooth(SmoothingOptions{Step: .25, MaxGap: 30, MaxAcceleration: 20, UseKinematics: true}); err != nil {
		t.Fatal("rounded north-bearing estimate became an invalid 360 degree report", err)
	}
}
