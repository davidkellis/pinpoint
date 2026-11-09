package search

import (
	"context"
	"math"
	"pinpoint/internal/geo"
	"testing"
)

func TestEnvelopeRetainsOffCenterTrueCurvesAndRejectsIncompatibleShape(t *testing.T) {
	c, s, ts, truth, _ := timingFixture()
	center := truth.pos.Add(geo.Vec{X: 80, Y: 100, Z: 60})
	for i, e := range c.Events {
		obs := prepareObservations(s, e)
		if !curveRangePossible(ts[i], center, 200, e, c, obs, 1700000000-1, 1700000000+1) {
			t.Fatal("discarded a true curve inside observer/time uncertainty")
		}
		for k := range obs {
			obs[k].hz += 80 * float64(k%2)
		}
		if curveRangePossible(ts[i], center, 200, e, c, obs, 1700000000-1, 1700000000+1) {
			t.Fatal("failed to exclude impossible alternating pitches")
		}
	}
	c.CurveEnvelope = true
	r, err := Run(context.Background(), c, s, ts)
	if err != nil || len(r.Candidates) == 0 {
		t.Fatalf("lost joint match: %v", err)
	}
	best := r.Candidates[0]
	if geo.ECEF(best.Lat, best.Lon, best.Height).Sub(truth.pos).Norm() > 100 {
		t.Fatal("lost known synthetic location")
	}
}

func TestSubsetScreenMatchesFullScorerAcrossPositionsAndClocks(t *testing.T) {
	c, s, ts, p, _ := timingFixture()
	for i, e := range c.Events {
		obs := prepareObservations(s, e)
		for x := -1000.; x <= 1000; x += 200 {
			for clock := -20.; clock <= 20; clock += 2 {
				pos := p.pos.Add(geo.Vec{Y: x})
				f, a, cov := scoreObservedTrack(ts[i], pos, s, e, 1700000000+clock, c.SoundSpeed, c.MaxGap, obs)
				var d FitDiagnostic
				f2, b, cov2 := scoreObservedTrackDiagnostic(ts[i], pos, s, e, 1700000000+clock, c.SoundSpeed, c.MaxGap, obs, &d)
				if math.IsInf(a, -1) != math.IsInf(b, -1) || !math.IsInf(a, -1) && (f != f2 || a != b || cov != cov2) {
					t.Fatal("safe prefilter changed an accepted fit")
				}
			}
		}
	}
}

func TestClockEnvelopeClosedEdgesAndUnrestrictedRefinement(t *testing.T) {
	c := DefaultConfig()
	c.ClockUncertainty = 120
	c.ClockStep = .5
	for bin := 0; bin < 60; bin++ {
		mask := uint64(1) << bin
		from := -120 + float64(bin)*4
		for _, x := range []float64{from, from + 2, from + 4} {
			if !clockMaskAllows(mask, c, x) {
				t.Fatal("discarded a closed-bin edge", bin, x)
			}
		}
		if clockMaskAllows(mask, c, from+4.5) {
			t.Fatal("retried excluded clock bin")
		}
	}
	if !clockMaskAllows(0, c, 80) {
		t.Fatal("refinement lost its full clock range")
	}
}
