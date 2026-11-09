package geo

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
)

// Terrain samples uncompressed SRTM .hgt tiles. Access from a single goroutine.
// HGT elevations are orthometric; callers must add a local geoid undulation.
type Terrain struct {
	Dir   string
	tiles map[string][]byte
}

func (t *Terrain) Height(lat, lon float64) (float64, error) {
	la, lo := int(math.Floor(lat)), int(math.Floor(lon))
	ns, ew := "N", "E"
	if la < 0 {
		ns = "S"
	}
	if lo < 0 {
		ew = "W"
	}
	name := fmt.Sprintf("%s%02d%s%03d.hgt", ns, int(math.Abs(float64(la))), ew, int(math.Abs(float64(lo))))
	if t.tiles == nil {
		t.tiles = map[string][]byte{}
	}
	data, ok := t.tiles[name]
	if !ok {
		var err error
		data, err = os.ReadFile(filepath.Join(t.Dir, name))
		if err != nil {
			return 0, err
		}
		t.tiles[name] = data
	}
	n := int(math.Sqrt(float64(len(data) / 2)))
	if (n != 1201 && n != 3601) || len(data) != 2*n*n {
		return 0, fmt.Errorf("invalid HGT size: %s", name)
	}
	x := (lon - float64(lo)) * float64(n-1)
	y := (float64(la+1) - lat) * float64(n-1)
	ix, iy := min(int(x), n-2), min(int(y), n-2)
	dx, dy := x-float64(ix), y-float64(iy)
	sum := 0.0
	for j := 0; j < 2; j++ {
		for i := 0; i < 2; i++ {
			w := []float64{1 - dx, dx}[i] * []float64{1 - dy, dy}[j]
			if w == 0 {
				continue
			}
			k := 2 * ((iy+j)*n + ix + i)
			v := int16(binary.BigEndian.Uint16(data[k : k+2]))
			if v == -32768 {
				return 0, fmt.Errorf("terrain void at %.6f,%.6f", lat, lon)
			}
			sum += w * float64(v)
		}
	}
	return sum, nil
}
