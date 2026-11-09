package tracks

import (
	_ "embed"
	"encoding/json"
	"strings"
)

//go:embed data/type-engines.json
var engineData []byte

var engineTypes = func() map[string]string {
	var data struct {
		Engines map[string]string `json:"engines"`
	}
	if err := json.Unmarshal(engineData, &data); err != nil {
		panic(err)
	}
	return data.Engines
}()

func EngineClass(code string) string { return engineTypes[strings.ToUpper(strings.TrimSpace(code))] }

type TypeFilterStats struct {
	ExcludedJets int `json:"excluded_jet_tracks"`
	KnownNonJets int `json:"known_nonjet_tracks"`
	Unknown      int `json:"unknown_type_tracks_retained"`
}

// ExcludeKnownJets deliberately retains missing/unrecognized types. A missing
// database entry is not evidence that an aircraft cannot produce a tonal pass.
func ExcludeKnownJets(ts []Track) ([]Track, TypeFilterStats) {
	stats := TypeFilterStats{}
	out := make([]Track, 0, len(ts))
	for _, t := range ts {
		switch EngineClass(t.TypeCode) {
		case "J":
			stats.ExcludedJets++
			continue
		case "P", "T":
			stats.KnownNonJets++
		default:
			stats.Unknown++
		}
		out = append(out, t)
	}
	return out, stats
}
