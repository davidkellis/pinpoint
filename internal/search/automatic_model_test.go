package search

import (
	"context"
	"os"
	"pinpoint/internal/geo"
	"testing"
)

func TestAutomaticCacheInvalidatesCorrectedOrNewTerrain(t *testing.T) {
	c, _, ts, _, _ := timingFixture()
	c.ObserverHeight = nil
	c.TerrainDir = t.TempDir()
	offset := 0.
	c.GeoidOffset = &offset
	o := DefaultAutomaticOptions(c)
	o.ControlShifts = nil
	a, err := automaticModelInputs(context.Background(), c, ts, o)
	if err != nil || len(a) == 0 {
		t.Fatal("missing terrain footprint", err)
	}
	key := bindModelInputs("measurements", a)
	if !a[0].Missing {
		t.Fatal("fixture tile unexpectedly exists")
	}
	if err = os.WriteFile(a[0].Path, []byte{0, 0, 1}, 0600); err != nil {
		t.Fatal(err)
	}
	b, err := automaticModelInputs(context.Background(), c, ts, o)
	if err != nil {
		t.Fatal(err)
	}
	if b[0].Missing || bindModelInputs("measurements", b) == key {
		t.Fatal("creating terrain reused stale geometry")
	}
	key = bindModelInputs("measurements", b)
	if err = os.WriteFile(a[0].Path, []byte{0, 0, 2}, 0600); err != nil {
		t.Fatal(err)
	}
	b, err = automaticModelInputs(context.Background(), c, ts, o)
	if err != nil || bindModelInputs("measurements", b) == key {
		t.Fatal("corrected elevations reused stale geometry", err)
	}
}

func TestTiledBudgetKeepsJointMatchAndCompleteRegion(t *testing.T) {
	c, s, ts, truth, _ := timingFixture()
	c.Staged = false
	c.Corridors = false
	c.Grid = 100
	c.Refine = 0
	c.FinalGrid = 0
	c.MaxGridPoints = 60
	r, err := RunTiled(context.Background(), c, s, ts, nil)
	if err != nil || len(r.Candidates) == 0 {
		t.Fatal("budget tiling lost synthetic joint match", err)
	}
	best := r.Candidates[0]
	if best.Events[0].Track != ts[0].ID || best.Events[1].Track != ts[1].ID || geo.ECEF(best.Lat, best.Lon, best.Height).Sub(truth.pos).Norm() > 160 {
		t.Fatal("wrong tiled joint solution")
	}
	minLat, maxLat, minLon, maxLon := 1e9, -1e9, 1e9, -1e9
	for _, p := range r.ScoreGrid {
		if !c.Bounds.Contains(p.Lat, p.Lon) {
			t.Fatal("tile escaped original rectangle")
		}
		minLat = min(minLat, p.Lat)
		maxLat = max(maxLat, p.Lat)
		minLon = min(minLon, p.Lon)
		maxLon = max(maxLon, p.Lon)
	}
	delta := c.Grid / 111320
	if minLat > c.Bounds.South+delta || maxLat < c.Bounds.North-delta || minLon > c.Bounds.West+delta || maxLon < c.Bounds.East-delta {
		t.Fatal("tiled search silently dropped a region")
	}
}
