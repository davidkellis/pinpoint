package geo

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"
)

func TestGeoidInterpolationAndSign(t *testing.T) {
	b := make([]byte, 56)
	for i, v := range []float64{30, -100, 1, 1} {
		binary.BigEndian.PutUint64(b[8*i:], math.Float64bits(v))
	}
	binary.BigEndian.PutUint32(b[32:], 2)
	binary.BigEndian.PutUint32(b[36:], 2)
	for i, v := range []float32{-32, -30, -28, -26} {
		binary.BigEndian.PutUint32(b[40+4*i:], math.Float32bits(v))
	}
	path := filepath.Join(t.TempDir(), "test.gtx")
	os.WriteFile(path, b, 0600)
	g, e := LoadGeoid(path)
	if e != nil {
		t.Fatal(e)
	}
	h, e := g.Height(30.5, -99.5)
	if e != nil || h != -29 {
		t.Fatalf("got %v, %v", h, e)
	}
	if _, e = g.Height(32, -99); e == nil {
		t.Fatal("extrapolated geoid")
	}
	if 200+h != 171 {
		t.Fatal("orthometric to ellipsoidal sign")
	}
}
