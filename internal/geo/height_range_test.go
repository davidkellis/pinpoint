package geo

import (
	"encoding/binary"
	"testing"
)

func TestHeightRangeIncludesInteriorPeakAndVoid(t *testing.T) {
	const n = 1201
	data := make([]byte, 2*n*n)
	k := 2 * (600*n + 600)
	binary.BigEndian.PutUint16(data[k:], 5000)
	terrain := Terrain{tiles: map[string][]byte{"N00E000.hgt": data}}
	b := Bounds{South: .49, North: .51, West: .49, East: .51}
	lo, hi, err := terrain.HeightRange(b)
	if err != nil || lo != 0 || hi != 5000 {
		t.Fatalf("missed interior peak: %g %g %v", lo, hi, err)
	}
	binary.BigEndian.PutUint16(data[k+4:], 32768)
	if _, _, err = terrain.HeightRange(b); err == nil {
		t.Fatal("accepted void in cell")
	}
}
