package geo

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"
)

func TestECEF(t *testing.T) {
	if got := ECEF(0, 0, 0); got.X != 6378137 || got.Y != 0 || got.Z != 0 {
		t.Fatal(got)
	}
	for _, p := range [][3]float64{{35.25, -100.5, 700}, {-45, 170, 20}, {80, -179, -30}} {
		lat, lon, h := Geodetic(ECEF(p[0], p[1], p[2]))
		if math.Abs(lat-p[0]) > 1e-8 || math.Abs(lon-p[1]) > 1e-8 || math.Abs(h-p[2]) > 1e-5 {
			t.Fatalf("round trip %v => %v,%v,%v", p, lat, lon, h)
		}
	}
}
func TestHGTInterpolationAndVoid(t *testing.T) {
	dir := t.TempDir()
	data := make([]byte, 1201*1201*2)
	// Row index as elevation tests north/south orientation and interpolation.
	for y := 0; y < 1201; y++ {
		for x := 0; x < 1201; x++ {
			binary.BigEndian.PutUint16(data[2*(y*1201+x):], uint16(y))
		}
	}
	path := filepath.Join(dir, "N35W101.hgt")
	if e := os.WriteFile(path, data, 0644); e != nil {
		t.Fatal(e)
	}
	terrain := Terrain{Dir: dir}
	got, e := terrain.Height(35.25, -100.5)
	if e != nil || math.Abs(got-900) > 1e-6 {
		t.Fatalf("got %v %v", got, e)
	}
	binary.BigEndian.PutUint16(terrain.tiles["N35W101.hgt"][2*(900*1201+600):], 0x8000)
	if _, e = terrain.Height(35.25, -100.5); e == nil {
		t.Fatal("accepted terrain void")
	}
	if _, e = terrain.Height(34.25, -100.5); e == nil {
		t.Fatal("silently assumed missing terrain is zero")
	}
}
