package geo

import (
	"encoding/json"
	"testing"
)

func TestDisplayBordersKeepStatesAndLeaveSearchMaskUnchanged(t *testing.T) {
	mask, _ := NewMask("TX")
	before := mask.Contains(31, -99)
	original, _ := json.Marshal(maskShapes)
	raw, err := StateBorders()
	if err != nil {
		t.Fatal(err)
	}
	var collection struct {
		Type     string
		Features []struct {
			Properties State
			Geometry   struct {
				Type        string
				Coordinates [][][2]float64
			}
		}
	}
	if err = json.Unmarshal(raw, &collection); err != nil {
		t.Fatal(err)
	}
	if collection.Type != "FeatureCollection" || len(collection.Features) != 49 {
		t.Fatal("missing state outlines")
	}
	found := false
	for _, f := range collection.Features {
		if f.Properties.Code == "TX" {
			found = true
			for _, line := range f.Geometry.Coordinates {
				if len(line) < 3 || line[0] != line[len(line)-1] {
					t.Fatal("unclosed state outline")
				}
				for _, p := range line {
					if p[0] > -90 || p[1] < 25 || p[1] > 37 {
						t.Fatal("reversed/incorrect boundary coordinates")
					}
				}
			}
		}
	}
	after, _ := json.Marshal(maskShapes)
	if string(original) != string(after) {
		t.Fatal("display simplification mutated search geometry")
	}
	if !found || !before || !mask.Contains(31, -99) || mask.Contains(35, -107) {
		t.Fatal("display altered search mask")
	}
}
