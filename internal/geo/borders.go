package geo

import (
	"encoding/json"
	"sync"
)

var bordersOnce sync.Once
var bordersJSON []byte
var bordersError error

// StateBorders returns cartographic outlines, separate from the full-resolution
// polygons used to constrain searches. The display-only simplification is at
// most 0.001 degrees; it never changes which locations are searched.
func StateBorders() ([]byte, error) {
	bordersOnce.Do(func() {
		if _, bordersError = NewMask("CONUS"); bordersError != nil {
			return
		}
		type geometry struct {
			Type        string         `json:"type"`
			Coordinates [][][2]float64 `json:"coordinates"`
		}
		type feature struct {
			Type       string   `json:"type"`
			Properties State    `json:"properties"`
			Geometry   geometry `json:"geometry"`
		}
		collection := struct {
			Type     string    `json:"type"`
			Features []feature `json:"features"`
		}{Type: "FeatureCollection"}
		for _, state := range States() {
			g := geometry{Type: "MultiLineString"}
			for _, ring := range maskShapes[state.Code] {
				line := simplifyBorder(ring, .001*.001)
				if len(line) > 2 {
					g.Coordinates = append(g.Coordinates, line)
				}
			}
			collection.Features = append(collection.Features, feature{"Feature", state, g})
		}
		bordersJSON, bordersError = json.Marshal(collection)
	})
	return bordersJSON, bordersError
}

func simplifyBorder(p [][2]float64, toleranceSquared float64) [][2]float64 {
	if len(p) < 3 {
		return p
	}
	a, b := p[0], p[len(p)-1]
	dx, dy := b[0]-a[0], b[1]-a[1]
	distance, index := toleranceSquared, 0
	for i := 1; i < len(p)-1; i++ {
		u := 0.0
		if dx*dx+dy*dy > 0 {
			u = max(0, min(1, ((p[i][0]-a[0])*dx+(p[i][1]-a[1])*dy)/(dx*dx+dy*dy)))
		}
		x, y := p[i][0]-a[0]-u*dx, p[i][1]-a[1]-u*dy
		if d := x*x + y*y; d > distance {
			distance, index = d, i
		}
	}
	if index == 0 {
		return [][2]float64{a, b}
	}
	left := simplifyBorder(p[:index+1], toleranceSquared)
	right := simplifyBorder(p[index:], toleranceSquared)
	return append(left[:len(left)-1:len(left)-1], right...)
}
