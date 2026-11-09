package search

import (
	"context"
	"reflect"
	"testing"

	"pinpoint/internal/acoustic"
	"pinpoint/internal/geo"
	"pinpoint/internal/tracks"
)

func TestJointRidgeAssignmentUsesAugmentingPathsAndTrainingOnly(t *testing.T) {
	q := func(id string, train, test float64) JointRidgeMatch {
		return JointRidgeMatch{Track: id, Aircraft: id, NormalizedTraining: train, Fit: CurveModelFit{Reserved: test}}
	}
	chosen, ok := assignRidgeAircraft([][]JointRidgeMatch{{q("A", .1, 0), q("B", .2, 0)}, {q("A", .15, 0)}})
	if !ok || chosen[0].Track != "B" || chosen[1].Track != "A" {
		t.Fatal("greedy assignment discarded a valid distinct-aircraft solution", chosen)
	}
	chosen, ok = assignRidgeAircraft([][]JointRidgeMatch{{q("A", .1, 100), q("B", .2, 0)}, {q("C", .05, 0)}})
	if !ok || chosen[0].Track != "A" {
		t.Fatal("reserved winner rescued training failure", chosen)
	}
	if _, ok = assignRidgeAircraft([][]JointRidgeMatch{{q("A", .1, 0)}, {q("A", .1, 0)}}); ok {
		t.Fatal("one aircraft used twice")
	}
}

func TestJointRidgesLockOneReceiverAndClockAcrossWholeCurves(t *testing.T) {
	h, tr, e, o := changingPitchFixture()
	second := tr
	second.ID = "second-aircraft"
	second.Points = append([]tracks.Point(nil), tr.Points...)
	velocity := geo.ENUVector(h.Lat, h.Lon, h.East, h.North, h.Up)
	for i := range second.Points {
		second.Points[i].Time += 200
		second.Points[i].Position = second.Points[i].Position.Add(velocity.Scale(200))
	}
	e2 := e
	e2.Start += 200
	e2.End += 200
	e2.Ridge = append([]acoustic.RidgeSample(nil), e.Ridge...)
	for i := range e2.Ridge {
		e2.Ridge[i].Time += 200
	}
	r, err := FitJointRidges(context.Background(), h, []tracks.Track{tr, second}, []acoustic.Event{e, e2}, o)
	if err != nil || !r.Valid || !r.ReservedPass || r.WorstTraining > .001 {
		t.Fatal("lost valid locked curve sequence", r, err)
	}
	h.Clock = 10
	wrong, err := FitJointRidges(context.Background(), h, []tracks.Track{tr, second}, []acoustic.Event{e, e2}, o)
	if err != nil || wrong.Valid && wrong.WorstTraining < .01 {
		t.Fatal("independently retimed curves or ignored common clock", wrong, err)
	}
}

func TestPreparedJointRidgesPreserveAllSelectedStatistics(t *testing.T) {
	h, tr, e, o := changingPitchFixture()
	tr, _, err := tr.Smooth(tracks.SmoothingOptions{Step: .1, MaxGap: 30, MaxAcceleration: 20})
	if err != nil {
		t.Fatal(err)
	}
	a, err := FitJointRidges(context.Background(), h, []tracks.Track{tr}, []acoustic.Event{e}, o)
	if err != nil {
		t.Fatal(err)
	}
	b, err := FitJointRidges(context.Background(), h, []tracks.Track{tr.PrepareSegments()}, []acoustic.Event{e}, o)
	if err != nil || !a.Valid || !reflect.DeepEqual(a, b) {
		t.Fatal("geometry optimization changed selected full-curve fit", a, b, err)
	}
}

func BenchmarkDenseJointRidge(b *testing.B) {
	h, tr, e, o := changingPitchFixture()
	tr, _, err := tr.Smooth(tracks.SmoothingOptions{Step: .1, MaxGap: 30, MaxAcceleration: 20})
	if err != nil {
		b.Fatal(err)
	}
	for _, prepared := range []bool{false, true} {
		name := "exhaustive"
		if prepared {
			tr = tr.PrepareSegments()
			name = "prepared"
		}
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_, err := FitJointRidges(context.Background(), h, []tracks.Track{tr}, []acoustic.Event{e}, o)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
