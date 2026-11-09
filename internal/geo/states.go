package geo

import (
	_ "embed"
	"encoding/json"
	"strings"
)

// Census 2024 cartographic boundary file, cb_2024_us_state_500k.zip.
// Bounds support the map and flight downloads; Mask clips candidate positions
// to state polygons. No town or known recording position is favored.
//
//go:embed states.json
var stateData []byte

type State struct {
	Code   string `json:"code"`
	Name   string `json:"name"`
	Bounds Bounds `json:"bounds"`
}

func States() []State {
	var states []State
	_ = json.Unmarshal(stateData, &states)
	return states
}

func StateBounds(name string) (Bounds, bool) {
	if name == "" {
		return Bounds{South: 24.39, North: 49.39, West: -124.85, East: -66.88}, true
	}
	for _, s := range States() {
		if strings.EqualFold(name, s.Code) || strings.EqualFold(name, s.Name) {
			return s.Bounds, true
		}
	}
	return Bounds{}, false
}
