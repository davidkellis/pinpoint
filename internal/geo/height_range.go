package geo

import (
	"encoding/binary"
	"fmt"
	"math"
)

// HeightRange includes every interpolation vertex touched by the rectangle.
// Bilinear interpolation cannot exceed these extrema. Missing tiles/voids are
// errors; callers doing only a conservative prefilter should retain that cell.
func (t *Terrain) HeightRange(b Bounds) (float64, float64, error) {
	low, high := math.Inf(1), math.Inf(-1)
	for la := int(math.Floor(b.South)); la <= int(math.Floor(b.North)); la++ {
		for lo := int(math.Floor(b.West)); lo <= int(math.Floor(b.East)); lo++ {
			// Height loads and validates this tile in the shared single-thread cache.
			if _, err := t.Height(float64(la)+.5, float64(lo)+.5); err != nil {
				return 0, 0, err
			}
			data := t.tiles[TileName(float64(la)+.5, float64(lo)+.5)]
			n := int(math.Sqrt(float64(len(data) / 2)))
			if n < 2 {
				return 0, 0, fmt.Errorf("missing terrain tile")
			}
			x0 := max(0, int(math.Floor((b.West-float64(lo))*float64(n-1))))
			x1 := min(n-1, int(math.Ceil((b.East-float64(lo))*float64(n-1))))
			y0 := max(0, int(math.Floor((float64(la+1)-b.North)*float64(n-1))))
			y1 := min(n-1, int(math.Ceil((float64(la+1)-b.South)*float64(n-1))))
			for y := y0; y <= y1; y++ {
				for x := x0; x <= x1; x++ {
					v := int16(binary.BigEndian.Uint16(data[2*(y*n+x):]))
					if v == -32768 {
						return 0, 0, fmt.Errorf("terrain void")
					}
					low = math.Min(low, float64(v))
					high = math.Max(high, float64(v))
				}
			}
		}
	}
	return low, high, nil
}

func (g *Geoid) HeightRange(b Bounds) (float64, float64, error) {
	x0, x1 := (b.West-g.lon)/g.dx, (b.East-g.lon)/g.dx
	y0, y1 := (b.South-g.lat)/g.dy, (b.North-g.lat)/g.dy
	if x0 < 0 || y0 < 0 || x1 > float64(g.cols-1) || y1 > float64(g.rows-1) {
		return 0, 0, fmt.Errorf("outside geoid")
	}
	low, high := math.Inf(1), math.Inf(-1)
	for y := int(math.Floor(y0)); y <= int(math.Ceil(y1)); y++ {
		for x := int(math.Floor(x0)); x <= int(math.Ceil(x1)); x++ {
			v := float64(math.Float32frombits(binary.BigEndian.Uint32(g.data[4*(y*g.cols+x):])))
			if !Finite(v) || math.Abs(v) > 150 {
				return 0, 0, fmt.Errorf("invalid geoid")
			}
			low = math.Min(low, v)
			high = math.Max(high, v)
		}
	}
	return low, high, nil
}
