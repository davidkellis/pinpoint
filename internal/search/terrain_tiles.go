package search

import (
	"context"
	"math"
	"pinpoint/internal/geo"
	"pinpoint/internal/tracks"
	"sort"
	"time"
)

// TerrainTiles covers corridor rectangles directly, including their boundaries.
// Preparing DEM data must not first allocate a statewide point grid or miss a
// small coastal region merely because a coarser preparation lattice skips it.
func TerrainTiles(ctx context.Context, c Config, ts []tracks.Track) ([]string, error) {
	names := map[string]bool{}
	add := func(b geo.Bounds) {
		b.South = math.Max(b.South, c.Bounds.South)
		b.North = math.Min(b.North, c.Bounds.North)
		b.West = math.Max(b.West, c.Bounds.West)
		b.East = math.Min(b.East, c.Bounds.East)
		if b.South > b.North || b.West > b.East {
			return
		}
		for y := math.Floor(b.South); y <= math.Floor(b.North); y++ {
			for x := math.Floor(b.West); x <= math.Floor(b.East); x++ {
				names[geo.TileName(y+.5, x+.5)] = true
			}
		}
	}
	if !c.Corridors {
		add(c.Bounds)
	} else {
		t, _ := time.Parse(time.RFC3339Nano, c.RecordingStart)
		start := float64(t.Unix()) + float64(t.Nanosecond())/1e9
		for _, tr := range ts {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			for i := 1; i < len(tr.Points); i++ {
				a, b := tr.Points[i-1], tr.Points[i]
				dt := b.Time - a.Time
				if dt <= 0 || dt > c.MaxGap {
					continue
				}
				for _, e := range c.Events {
					if b.Time < start-c.ClockUncertainty+e.Start-e.MaxSlant/c.SoundSpeed-10 || a.Time > start+c.ClockUncertainty+e.End+10 {
						continue
					}
					speed := b.Position.Sub(a.Position).Norm() / dt
					if speed < e.MinSpeed || speed > e.MaxSpeed {
						continue
					}
					pad := (e.MaxSlant + 2*c.Grid) / 110000
					dx := pad / math.Cos(math.Max(math.Abs(a.Lat), math.Abs(b.Lat))*math.Pi/180)
					add(geo.Bounds{South: math.Min(a.Lat, b.Lat) - pad, North: math.Max(a.Lat, b.Lat) + pad, West: math.Min(a.Lon, b.Lon) - dx, East: math.Max(a.Lon, b.Lon) + dx})
				}
			}
		}
	}
	out := make([]string, 0, len(names))
	for name := range names {
		out = append(out, name)
	}
	sort.Strings(out)
	return out, ctx.Err()
}
