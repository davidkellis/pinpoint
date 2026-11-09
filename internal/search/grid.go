package search

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"

	"pinpoint/internal/geo"
	"pinpoint/internal/tracks"
)

// GridLocations generates a deterministic lattice, optionally restricted to
// aircraft corridors during the event windows. It never coarsens the requested
// spacing to hide a resource limit. Segment envelopes include acoustic delay.
func GridLocations(ctx context.Context, c Config, ts []tracks.Track) ([][2]float64, error) {
	mask, err := geo.NewMask(c.RegionMask)
	if err != nil {
		return nil, err
	}
	dy := c.Grid / 111320
	dx := dy / math.Cos((c.Bounds.South+c.Bounds.North)*math.Pi/360)
	ny, nx := int(math.Ceil((c.Bounds.North-c.Bounds.South)/dy)), int(math.Ceil((c.Bounds.East-c.Bounds.West)/dx))
	limit := c.MaxGridPoints
	if limit == 0 {
		limit = 250000
	}
	if !c.Corridors && float64(nx+1)*float64(ny+1) > float64(limit) {
		return nil, fmt.Errorf("region exceeds %d grid locations; narrow bounds or increase grid spacing", limit)
	}
	seen := map[[2]int]bool{}
	add := func(y0, y1, x0, x1 int) error {
		for y := max(0, y0); y <= min(ny, y1); y++ {
			if err := ctx.Err(); err != nil {
				return err
			}
			for x := max(0, x0); x <= min(nx, x1); x++ {
				if seen[[2]int{y, x}] {
					continue
				}
				if !mask.Contains(min(c.Bounds.North, c.Bounds.South+float64(y)*dy), min(c.Bounds.East, c.Bounds.West+float64(x)*dx)) {
					continue
				}
				seen[[2]int{y, x}] = true
				if len(seen) > limit {
					return fmt.Errorf("flight corridors exceed %d grid locations; narrow the time window or search area", limit)
				}
			}
		}
		return nil
	}
	if !c.Corridors {
		if e := add(0, ny, 0, nx); e != nil {
			return nil, e
		}
	} else {
		t, err := time.Parse(time.RFC3339Nano, c.RecordingStart)
		if err != nil {
			return nil, err
		}
		start := float64(t.Unix()) + float64(t.Nanosecond())/1e9
		for _, tr := range ts {
			for i := 1; i < len(tr.Points); i++ {
				a, b := tr.Points[i-1], tr.Points[i]
				dt := b.Time - a.Time
				if dt <= 0 || dt > c.MaxGap {
					continue
				}
				for _, event := range c.Events {
					if b.Time < start-c.ClockUncertainty+event.Start-event.MaxSlant/c.SoundSpeed-10 || a.Time > start+c.ClockUncertainty+event.End+10 {
						continue
					}
					speed := b.Position.Sub(a.Position).Norm() / dt
					if speed < event.MinSpeed || speed > event.MaxSpeed {
						continue
					}
					pad := (event.MaxSlant + c.Grid*2) / 110000
					padlon := pad / math.Cos(math.Max(math.Abs(a.Lat), math.Abs(b.Lat))*math.Pi/180)
					y0, y1 := int(math.Floor((min(a.Lat, b.Lat)-pad-c.Bounds.South)/dy)), int(math.Ceil((max(a.Lat, b.Lat)+pad-c.Bounds.South)/dy))
					x0, x1 := int(math.Floor((min(a.Lon, b.Lon)-padlon-c.Bounds.West)/dx)), int(math.Ceil((max(a.Lon, b.Lon)+padlon-c.Bounds.West)/dx))
					if e := add(y0, y1, x0, x1); e != nil {
						return nil, e
					}
				}
			}
		}
	}
	keys := make([][2]int, 0, len(seen))
	for k := range seen {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		return keys[i][0] < keys[j][0] || keys[i][0] == keys[j][0] && keys[i][1] < keys[j][1]
	})
	out := make([][2]float64, 0, len(keys))
	for _, k := range keys {
		out = append(out, [2]float64{min(c.Bounds.North, c.Bounds.South+float64(k[0])*dy), min(c.Bounds.East, c.Bounds.West+float64(k[1])*dx)})
	}
	return out, nil
}
