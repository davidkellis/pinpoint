package geo

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
)

// Geoid reads the PROJ GTX grid format (big endian header and float32 samples).
// EGM96 undulation converts orthometric terrain heights to ellipsoidal heights:
// h = H + N. The grid is small enough to keep in memory.
type Geoid struct {
	data             []byte
	lat, lon, dy, dx float64
	rows, cols       int
}

func LoadGeoid(path string) (*Geoid, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(b) < 40 {
		return nil, fmt.Errorf("truncated geoid grid")
	}
	d := func(i int) float64 { return math.Float64frombits(binary.BigEndian.Uint64(b[i : i+8])) }
	g := &Geoid{b[40:], d(0), d(8), d(16), d(24), int(binary.BigEndian.Uint32(b[32:36])), int(binary.BigEndian.Uint32(b[36:40]))}
	if !Finite(g.lat) || !Finite(g.lon) || !Finite(g.dx) || !Finite(g.dy) || g.dx <= 0 || g.dy <= 0 || g.rows < 2 || g.cols < 2 || g.rows > 10000 || g.cols > 10000 || len(g.data) != 4*g.rows*g.cols {
		return nil, fmt.Errorf("invalid geoid grid")
	}
	return g, nil
}

func (g *Geoid) Height(lat, lon float64) (float64, error) {
	x, y := (lon-g.lon)/g.dx, (lat-g.lat)/g.dy
	if x < 0 || y < 0 || x > float64(g.cols-1) || y > float64(g.rows-1) {
		return 0, fmt.Errorf("outside geoid grid")
	}
	i, j := min(int(x), g.cols-2), min(int(y), g.rows-2)
	dx, dy := x-float64(i), y-float64(j)
	v := 0.0
	for b := 0; b < 2; b++ {
		for a := 0; a < 2; a++ {
			k := 4 * ((j+b)*g.cols + i + a)
			h := float64(math.Float32frombits(binary.BigEndian.Uint32(g.data[k : k+4])))
			if !Finite(h) || math.Abs(h) > 150 {
				return 0, fmt.Errorf("invalid geoid sample")
			}
			v += h * []float64{1 - dx, dx}[a] * []float64{1 - dy, dy}[b]
		}
	}
	return v, nil
}
