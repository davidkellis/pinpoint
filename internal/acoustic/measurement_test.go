package acoustic

import (
	"math"
	"testing"
)

func TestJointSelectionRequiresCurvatureAndBoundsWholeFlyover(t *testing.T) {
	s := &Spectrum{SampleRate: 1024, Window: 2048, Hop: 512}
	var pass, linear, constant, partial []ridgePoint
	// Closed-form emission time, independent of the fitter's Newton solver.
	const v, c, d, f0 = 80.0, 343.0, 2000.0, 130.0
	for i := 0; i < 220; i++ {
		tr := s.Time(i) - 50
		te := (c*c*tr - math.Sqrt(c*c*v*v*tr*tr+(c*c-v*v)*d*d)) / (c*c - v*v)
		hz := f0 * c / (c + v*v*te/math.Hypot(d, v*te))
		p := ridgePoint{frame: i, hz: hz, value: 20}
		pass = append(pass, p)
		linear = append(linear, ridgePoint{frame: i, hz: 150 - s.Time(i)*.1})
		constant = append(constant, ridgePoint{frame: i, hz: 130})
		if i < 75 {
			partial = append(partial, p)
		}
	}
	m := fitRidge(s, pass)
	if !m.Usable {
		t.Fatalf("physical full flyover rejected: %+v", m)
	}
	if math.Abs(m.CPA-(50+d/c)) > m.CPATolerance || m.MinSpeed > v || m.MaxSpeed < v || m.MinSlant > d || m.MaxSlant < d || m.FMin > f0 || m.FMax < f0 {
		t.Fatalf("true parameters outside estimated ranges: %+v", m)
	}
	for _, points := range [][]ridgePoint{linear, constant, partial} {
		if m := fitRidge(s, points); m.Usable {
			t.Fatalf("unidentifiable curve promoted: %+v", m)
		}
	}
	events := make([]Event, 12)
	for i := range events {
		events[i] = Event{Start: float64(i * 200), Measurement: &m, Partial: true}
	}
	if len(JointEvents(events)) != 12 {
		t.Fatal("joint selection truncated usable events")
	}
	if !events[0].Partial || JointEvents(events)[0].Partial {
		t.Fatal("selection mutated proposals or failed to measure selected event")
	}
}

func TestJointSelectionDoesNotTreatIdealFitAsTrajectoryBounds(t *testing.T) {
	// A measured curve may be compatible with a real turning/climbing flight
	// whose speed, distance, source tone and CPA differ from the ideal fit.
	m := &RidgeMeasurement{Usable: true, CPA: 70, CPATolerance: 5,
		FMin: 102, FMax: 108, MinSpeed: 68, MaxSpeed: 114,
		MinSlant: 2700, MaxSlant: 8100}
	proposal := Event{Start: 10, End: 130, Partial: true, Measurement: m,
		FMin: 10, FMax: 250, MinSpeed: 15, MaxSpeed: 300, MaxSlant: 15000}
	e := JointEvents([]Event{proposal})[0]
	if e.FMin > 101 || e.FMax < 101 || e.MinSpeed > 124 || e.MaxSpeed < 124 || e.MinSlant > 10800 || e.MaxSlant < 10800 || math.Abs(79-e.CPATime) > e.CPATolerance {
		t.Fatalf("automatic selection excluded a plausible nonideal trajectory: %+v", e)
	}
	if e.CPATime-e.CPATolerance != proposal.Start || e.CPATime+e.CPATolerance != proposal.End {
		t.Fatal("CPA gate must cover the observed complete curve")
	}
	if *m != *e.Measurement || !proposal.Partial {
		t.Fatal("selection changed the reported measurement or proposal")
	}
}
