package tracks

import (
	"math"
	"testing"

	"pinpoint/internal/geo"
)

func denseTrajectory() Track {
	t := Track{ID: "dense-independent-trajectory"}
	for sec := -1800.; sec <= 1800; sec += .25 {
		t.Points = append(t.Points, Point{Time: 1700000000 + sec, Position: geo.Vec{X: sec * 95, Y: 3000 + 300*math.Sin(sec/300), Z: 800 + 2*sec}})
	}
	return t
}

func TestPreparedSegmentsPreserveArrivalPhysicsAndApproachExactly(t *testing.T) {
	raw := denseTrajectory()
	for _, mode := range []string{"subsonic", "gap", "supersonic"} {
		t.Run(mode, func(t *testing.T) {
			tr := raw
			tr.Points = append([]Point(nil), raw.Points...)
			if mode == "gap" {
				tr.Points = append(tr.Points[:7100], tr.Points[7300:]...)
			}
			if mode == "supersonic" {
				tr.Points[7000].Position.X += 10000
			}
			prepared := tr.PrepareSegments()
			for _, observer := range []geo.Vec{{}, {X: 200, Y: -300, Z: 120}} {
				for _, bounds := range [][2]float64{{-30, 90}, {-1900, -1850}, {1850, 1900}, {-1000, 1000}, {0, 0}, {-5, -5}, {math.Inf(-1), math.Inf(1)}} {
					from, to := 1700000000+bounds[0], 1700000000+bounds[1]
					a, yes := tr.ApproachWithin(observer, from, to, 343, 30)
					b, ok := prepared.ApproachWithin(observer, from, to, 343, 30)
					if yes != ok || yes && a != b {
						t.Fatal("prepared approach changed geometry", mode, bounds, a, b, yes, ok)
					}
					for _, radius := range []float64{0, 1000, 8000, 15000, 25000} {
						want := yes && a.Distance <= radius
						if tr.WithinRange(observer, from, to, radius, 343, 30) != want || prepared.WithinRange(observer, from, to, radius, 343, 30) != want {
							t.Fatal("early distance gate disagreed with complete minimum", bounds, radius, a)
						}
					}
				}
				for sec := -60.; sec < 100; sec += .375 {
					a, d, yes := tr.Received(observer, 1700000000+sec, 343, 30)
					b, e, ok := prepared.Received(observer, 1700000000+sec, 343, 30)
					if yes != ok || yes && (a != b || d != e) {
						t.Fatal("prepared solver changed received sound", a, b, d, e, yes, ok)
					}
				}
			}
			if tr.segments != nil || tr.receiver != nil {
				t.Fatal("modified shared trajectory")
			}
		})
	}
}

func TestPreparedApproachKeepsBothSegmentsAtExactArrivalVertex(t *testing.T) {
	tr := Track{Points: []Point{{Time: -10, Position: geo.Vec{X: -900, Y: 1500}}, {Time: 0, Position: geo.Vec{Y: 1500}}, {Time: 10, Position: geo.Vec{X: 1000, Y: 1500}}}}
	arrival := 1500. / 343
	a, yes := tr.ApproachWithin(geo.Vec{}, arrival, arrival, 343, 30)
	b, ok := tr.PrepareSegments().ApproachWithin(geo.Vec{}, arrival, arrival, 343, 30)
	if yes != ok || a != b || a.Speed != 90 {
		t.Fatal("changed endpoint tie/segment velocity", a, b, yes, ok)
	}
}

func BenchmarkDenseArrivalInterval(b *testing.B) {
	tr := denseTrajectory()
	for _, cached := range []bool{false, true} {
		name := "exhaustive"
		if cached {
			tr = tr.PrepareSegments()
			name = "prepared"
		}
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				tr.ApproachWithin(geo.Vec{}, 1700000000-30, 1700000000+90, 343, 30)
			}
		})
	}
}
