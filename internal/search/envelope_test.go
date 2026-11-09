package search

import (
	"context"
	"math/rand"
	"reflect"
	"testing"

	"pinpoint/internal/geo"
	"pinpoint/internal/tracks"
)

func TestEnvelopeRetainsInteriorApproach(t *testing.T) {
	// Endpoints can both be outside the audible radius while the segment
	// passes directly overhead. Endpoint-distance filtering would lose this.
	rng := rand.New(rand.NewSource(27))
	for trial := 0; trial < 100; trial++ {
		p := geo.ECEF(35+rng.Float64()*10, -80+rng.Float64()*10, 100)
		a := p.Add(geo.Vec{X: -20000, Y: 500, Z: 500})
		b := p.Add(geo.Vec{X: 20000, Y: 500, Z: 500})
		track := tracks.Track{ID: "crossing", Points: []tracks.Point{{Time: 0, Position: a}, {Time: 400, Position: b}}}
		ts := []tracks.Track{track}
		if len(nearbyTracks(p, ts, trackEnvelopes(ts), 1000)) != 1 {
			t.Fatal("lost an interior closest approach")
		}
		if len(nearbyTracks(p.Add(geo.Vec{Z: 20000}), ts, trackEnvelopes(ts), 1000)) != 0 {
			t.Fatal("distant track was not pruned")
		}
	}
}

func TestEnvelopeSearchMatchesUnfilteredScoring(t *testing.T) {
	c, s, ts := scoreGridFixture()
	c.ClockUncertainty = 3
	c.ClockStep = 0.5
	for _, row := range s.Rows {
		for i := range row {
			row[i] = 8
		}
	}
	far := tracks.Track{ID: "distant", Points: append([]tracks.Point(nil), ts[0].Points...)}
	for i := range far.Points {
		far.Points[i].Position.Z += 100000
	}
	ts = append(ts, far)
	p := observer{pos: geo.ECEF(0, 0, 0)}
	a, gridA, okA := evaluatePoint(context.Background(), p, c, s, ts, 1700000000)
	near := nearbyTracks(p.pos, ts, trackEnvelopes(ts), 10000)
	b, gridB, okB := evaluatePoint(context.Background(), p, c, s, near, 1700000000)
	if len(near) != 1 || okA != okB || !reflect.DeepEqual(a, b) || !reflect.DeepEqual(gridA, gridB) {
		t.Fatal("distance pruning changed the winning fit or score grid")
	}
}

func TestCellIndexEqualsFullEnvelopeScan(t *testing.T) {
	rng := rand.New(rand.NewSource(58))
	var ts []tracks.Track
	for i := 0; i < 120; i++ {
		a := geo.Vec{X: rng.NormFloat64() * 60000, Y: rng.NormFloat64() * 60000, Z: rng.NormFloat64() * 60000}
		b := a.Add(geo.Vec{X: rng.NormFloat64() * 30000, Y: rng.NormFloat64() * 30000, Z: rng.NormFloat64() * 30000})
		ts = append(ts, tracks.Track{Points: []tracks.Point{{Position: a}, {Position: b}}})
	}
	ts = append(ts, tracks.Track{Points: []tracks.Point{{Position: geo.Vec{X: -1e6, Y: -1e6, Z: -1e6}}, {Position: geo.Vec{X: 1e6, Y: 1e6, Z: 1e6}}}})
	idx := newTrackIndex(ts, 10000)
	if len(idx.large) != 1 {
		t.Fatal("large-box memory guard not exercised")
	}
	for i := 0; i < 300; i++ {
		p := geo.Vec{X: rng.NormFloat64() * 100000, Y: rng.NormFloat64() * 100000, Z: rng.NormFloat64() * 100000}
		want := nearbyTracks(p, ts, idx.envelopes, 10000)
		got := idx.nearby(p)
		if !reflect.DeepEqual(want, got) {
			t.Fatal("cell index changed candidates or their order")
		}
	}
}
