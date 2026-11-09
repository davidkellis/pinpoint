package geo

import "testing"

func TestStatePolygonExcludesBoundingBoxNeighbors(t *testing.T) {
	m, e := NewMask("Texas")
	if e != nil {
		t.Fatal(e)
	}
	for _, p := range [][2]float64{{32.6, -97.6}, {30.27, -97.74}, {27.5, -99.5}} {
		if !m.Contains(p[0], p[1]) {
			t.Fatal("lost Texas location", p)
		}
	}
	for _, p := range [][2]float64{{36.278897, -97.068741}, {35.47, -97.52}, {31, -105.9}} {
		if m.Contains(p[0], p[1]) {
			t.Fatal("included Oklahoma/Mexico location", p)
		}
	}
	all, e := NewMask("CONUS")
	if e != nil {
		t.Fatal(e)
	}
	if !all.Contains(35.47, -97.52) || all.Contains(31, -105.9) {
		t.Fatal("continental US mask incorrect")
	}
}
