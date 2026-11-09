package search

import (
	"context"
	"math"
	"testing"

	"pinpoint/internal/acoustic"
	"pinpoint/internal/geo"
	"pinpoint/internal/tracks"
)

// The first north/south flight sounds identical at mirror positions east and
// west of its track. The second east/west flight resolves that ambiguity only
// when both curves retain one clock. Frequencies use a closed-form retarded
// time, independent of the trajectory solver used by search.
func timingFixture() (Config, *acoustic.Spectrum, []tracks.Track, observer, observer) {
	c, _, _ := scoreGridFixture()
	c.ClockUncertainty, c.ClockStep = 20, 1
	c.Bounds = geo.Bounds{South: -.002, North: .002, West: -.010, East: .010}
	c.Grid, c.DenseGrid, c.Refine, c.FinalGrid = 200, 100, 50, 10
	c.Staged = true
	s := &acoustic.Spectrum{Version: 1, SampleRate: 1024, Window: 1024, Hop: 1024, FirstBin: 40, Rows: make([][]float32, 150)}
	for i := range s.Rows {
		s.Rows[i] = make([]float32, 220)
	}
	root := geo.ECEF(0, 0, 0)
	truth := observer{lat: 0, lon: 600.0 / 111320, pos: geo.ECEF(0, 600.0/111320, 0)}
	mirror := observer{lat: 0, lon: -truth.lon, pos: geo.ECEF(0, -truth.lon, 0)}
	ts := []tracks.Track{{ID: "north-south"}, {ID: "east-west"}}
	c.Events = nil
	for k, tr := range []struct {
		origin, v           geo.Vec
		cpa, tone, from, to float64
	}{
		{root.Add(geo.Vec{X: 1500}), geo.Vec{Z: 85}, 20, 130, 5.5, 45.5},
		{root.Add(geo.Vec{X: 1200, Z: 1000}), geo.Vec{Y: 80}, 95, 160, 87.5, 127.5},
	} {
		for sec := -100.; sec <= 300; sec += 2 {
			p := tr.origin.Add(tr.v.Scale(sec - tr.cpa))
			lat, lon, h := geo.Geodetic(p)
			ts[k].Points = append(ts[k].Points, tracks.Point{Time: 1700000000 + sec, Lat: lat, Lon: lon, Alt: h, Position: p})
		}
		e := acoustic.Event{Start: tr.from, End: tr.to, CPATime: (tr.from + tr.to) / 2, Partial: true, FMin: tr.tone * .8, FMax: tr.tone * 1.2, FStep: .25, Harmonics: []int{1}, MinSpeed: 15, MaxSpeed: 180, MaxSlant: 10000}
		r := tr.origin.Sub(truth.pos)
		v2 := tr.v.Dot(tr.v)
		u := -r.Dot(tr.v) / v2
		d := r.Add(tr.v.Scale(u)).Norm()
		cpa := tr.cpa + u
		for frame, row := range s.Rows {
			t := s.Time(frame)
			if t < e.Start || t > e.End {
				continue
			}
			received := t - cpa
			const sound = 343.
			te := (sound*sound*received - math.Sqrt(sound*sound*v2*received*received+(sound*sound-v2)*d*d)) / (sound*sound - v2)
			hz := tr.tone * sound / (sound + v2*te/math.Sqrt(d*d+v2*te*te))
			e.Ridge = append(e.Ridge, acoustic.RidgeSample{Time: t, Hz: hz})
			for b := range row {
				delta := (s.Hz(b) - hz) / .8
				row[b] = float32(25 * math.Exp(-delta*delta))
			}
		}
		c.Events = append(c.Events, e)
	}
	return c, s, ts, truth, mirror
}

func TestJointTimingDisambiguatesLocationWithoutMeasuredCPAs(t *testing.T) {
	c, s, ts, truth, mirror := timingFixture()
	ctx := context.Background()
	if _, err := c.Validate(s); err != nil {
		t.Fatal(err)
	}
	for _, e := range c.Events {
		one := c
		one.Staged = false
		one.Events = []acoustic.Event{e}
		if _, _, ok := evaluatePoint(ctx, mirror, one, s, ts, 1700000000); !ok {
			t.Fatal("fixture must admit each event at the wrong mirror with independent clocks")
		}
	}
	for _, staged := range []bool{false, true} {
		c.Staged = staged
		good, _, ok := evaluatePoint(ctx, truth, c, s, ts, 1700000000)
		if !ok || good.ClockOffset != 0 || len(good.Events) != 2 || good.Events[0].Track == good.Events[1].Track {
			t.Fatalf("true shared-clock sequence lost: %+v", good)
		}
		if _, _, ok = evaluatePoint(ctx, mirror, c, s, ts, 1700000000); ok {
			t.Fatal("wrong location rescued by separate event clocks")
		}
	}
	c.Staged = true
	r, err := Run(ctx, c, s, ts)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Candidates) == 0 {
		t.Fatal("joint partial-curve localization found no candidate")
	}
	best := r.Candidates[0]
	distance := geo.ECEF(best.Lat, best.Lon, best.Height).Sub(truth.pos).Norm()
	if distance > 100 || r.FinalGrid != 10 {
		t.Fatalf("did not localize/refine the shared-clock sequence: distance %.1fm, final grid %g", distance, r.FinalGrid)
	}
}

func TestJointTimingRejectsChangedFlightSpacingAndReusedTrack(t *testing.T) {
	c, s, ts, truth, _ := timingFixture()
	ctx := context.Background()
	for i := range ts[1].Points {
		ts[1].Points[i].Time += 20
	}
	for _, e := range c.Events {
		one := c
		one.Events = []acoustic.Event{e}
		one.Staged = false
		if _, _, ok := evaluatePoint(ctx, truth, one, s, ts, 1700000000); !ok {
			t.Fatal("each shifted flight should still fit separately")
		}
	}
	if _, _, ok := evaluatePoint(ctx, truth, c, s, ts, 1700000000); ok {
		t.Fatal("accepted incompatible inter-flight spacing")
	}
	// Even outside staged mode, two hypotheses for the same track are not two
	// independent flyovers, regardless of the sum of their acoustic scores.
	c.Staged = false
	c.Events = []acoustic.Event{c.Events[0], c.Events[0]}
	if _, _, ok := evaluatePoint(ctx, truth, c, s, ts[:1], 1700000000); ok {
		t.Fatal("counted one trajectory twice")
	}
}

func TestPartialGeometryUsesArrivalSupportNotPlaceholderCPA(t *testing.T) {
	c, _, ts, p, _ := timingFixture()
	c.Events[0].CPATime = -10000
	c.Events[1].CPATime = 10000
	if !sharedCPA(geometryIntervals(p, c, ts, 1700000000, 0)) {
		t.Fatal("invented partial CPA excluded physically possible samples")
	}
	for i := range ts[1].Points {
		ts[1].Points[i].Time += 1000
	}
	if sharedCPA(geometryIntervals(p, c, ts, 1700000000, 0)) {
		t.Fatal("unavailable received sound passed common-clock geometry")
	}
}
