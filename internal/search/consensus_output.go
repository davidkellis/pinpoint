package search

import (
	"os"
	"path/filepath"
	"sort"

	"pinpoint/internal/acoustic"
	"pinpoint/internal/mapview"
)

// consensusMapLimit caps embedded points so statewide maps stay a few MB.
const consensusMapLimit = 50000

type consensusMapPoint struct {
	Lat     float64 `json:"lat"`
	Lon     float64 `json:"lon"`
	Spacing float64 `json:"grid_m"`
	Valid   bool    `json:"valid"`
	Worst   float64 `json:"worst"`
	Clock   float64 `json:"clock_s"`
	Curves  int     `json:"curves"`
}

// WriteConsensusResult writes consensus.json and a portable consensus.html map to dir.
// Only the best valid points (up to a fixed cap) are embedded in the map.
func WriteConsensusResult(dir string, r ConsensusResult, curves []acoustic.LocatableCurve, extra map[string]any) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	out := map[string]any{"result": r, "selected_curves": curves}
	for k, v := range extra {
		out[k] = v
	}
	if err := JSON(filepath.Join(dir, "consensus.json"), out); err != nil {
		return err
	}
	var pts []consensusMapPoint
	for _, p := range r.Points {
		if p.Valid {
			pts = append(pts, consensusMapPoint{Lat: p.Lat, Lon: p.Lon, Spacing: p.Spacing, Valid: true, Worst: p.Worst, Clock: p.Clock, Curves: p.Explains})
		}
	}
	sort.SliceStable(pts, func(i, j int) bool { return pts[i].Worst < pts[j].Worst })
	if len(pts) > consensusMapLimit {
		pts = pts[:consensusMapLimit]
	}
	page, err := mapview.RenderConsensus(map[string]any{"result": r, "points": pts, "curves": curves})
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "consensus.html"), page, 0644)
}
