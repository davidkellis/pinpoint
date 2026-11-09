package search

import (
	"context"
	"math"
	"pinpoint/internal/acoustic"
	"pinpoint/internal/geo"
	"pinpoint/internal/tracks"
	"testing"
)

// A partial flyover with a measured ridge, 61 possible recording clocks, and
// nearby distractor trajectories exercises the automatic search's hot path.
func benchmarkPoint() (observer, Config, *acoustic.Spectrum, []tracks.Track, float64) {
	c, s, _ := scoreGridFixture()
	c.ClockUncertainty = 30
	e := c.Events[0]
	e.Partial = true
	e.FMin = 80
	e.FMax = 200
	e.FStep = .5
	origin := geo.ECEF(0, 0, 0)
	const epoch = 1700000000.0
	const sound = 343.0
	const speed = 85.0
	const height = 1500.0
	for frame, row := range s.Rows {
		heard := s.Time(frame) - 20
		te := (sound*sound*heard - math.Sqrt(sound*sound*speed*speed*heard*heard+(sound*sound-speed*speed)*height*height)) / (sound*sound - speed*speed)
		hz := 130 * sound / (sound + speed*speed*te/math.Hypot(height, speed*te))
		for k := range row {
			delta := (s.Hz(k) - hz) / .6
			row[k] = float32(20 * math.Exp(-delta*delta))
		}
		if s.Time(frame) >= e.Start && s.Time(frame) <= e.End {
			e.Ridge = append(e.Ridge, acoustic.RidgeSample{Time: s.Time(frame), Hz: hz})
		}
	}
	c.Events = []acoustic.Event{e}
	ts := make([]tracks.Track, 3)
	for k := range ts {
		ts[k].ID = string(rune('A' + k))
		for sec := -100; sec <= 100; sec += 2 {
			p := origin.Add(geo.Vec{X: height + float64(k)*700, Y: (float64(sec) - 20) * speed, Z: float64(k) * 500})
			lat, lon, alt := geo.Geodetic(p)
			ts[k].Points = append(ts[k].Points, tracks.Point{Time: epoch + float64(sec), Position: p, Lat: lat, Lon: lon, Alt: alt})
		}
	}
	return observer{pos: origin}, c, s, ts, epoch
}
func BenchmarkAutomaticPoint(b *testing.B) {
	p, c, s, ts, epoch := benchmarkPoint()
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		evaluatePoint(ctx, p, c, s, ts, epoch)
	}
}
func BenchmarkAutomaticTrack(b *testing.B) {
	p, c, s, ts, epoch := benchmarkPoint()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		scoreTrack(ts[0], p.pos, s, c.Events[0], epoch, 343, 30)
	}
}
