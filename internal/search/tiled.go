package search

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"

	"pinpoint/internal/acoustic"
	"pinpoint/internal/tracks"
)

// RunTiled respects the grid budget per tile without silently coarsening or
// dropping a region. Every tile performs the same geometry, scoring and local
// refinement. Failures other than an explicit grid limit remain failures.
func RunTiled(ctx context.Context, c Config, s *acoustic.Spectrum, ts []tracks.Track, progress func(string, int, int)) (Report, error) {
	var visit func(Config, int) (Report, error)
	visit = func(q Config, depth int) (Report, error) {
		if err := ctx.Err(); err != nil {
			return Report{}, err
		}
		r, err := RunWithProgress(ctx, q, s, ts, progress)
		if err == nil {
			return r, nil
		}
		if depth >= 12 || !(strings.Contains(err.Error(), "grid locations") || strings.Contains(err.Error(), "surviving regions exceed")) {
			return r, err
		}
		a, b := q, q
		if (q.Bounds.East-q.Bounds.West)*math.Cos((q.Bounds.North+q.Bounds.South)*math.Pi/360) > q.Bounds.North-q.Bounds.South {
			mid := (q.Bounds.West + q.Bounds.East) / 2
			a.Bounds.East = mid
			b.Bounds.West = mid
		} else {
			mid := (q.Bounds.South + q.Bounds.North) / 2
			a.Bounds.North = mid
			b.Bounds.South = mid
		}
		if progress != nil {
			progress(fmt.Sprintf("splitting region (depth %d)", depth+1), 0, 2)
		}
		x, err := visit(a, depth+1)
		if err != nil {
			return x, err
		}
		y, err := visit(b, depth+1)
		if err != nil {
			return y, err
		}
		return mergeTiles(q, x, y), nil
	}
	return visit(c, 0)
}

func mergeTiles(c Config, a, b Report) Report {
	r := a
	r.Rejections = map[string]int{}
	for k, v := range a.Rejections {
		r.Rejections[k] += v
	}
	for k, v := range b.Rejections {
		r.Rejections[k] += v
	}
	r.Bounds = c.Bounds
	r.Evaluated += b.Evaluated
	r.GeometryEvaluated += b.GeometryEvaluated
	r.GeometrySurvived += b.GeometrySurvived
	r.ScoreGrid = append(r.ScoreGrid, b.ScoreGrid...)
	r.RefinementPasses = append(r.RefinementPasses, b.RefinementPasses...)
	r.FinalGrid = math.Max(r.FinalGrid, b.FinalGrid)
	r.Candidates = append(r.Candidates, b.Candidates...)
	sortCandidates(r.Candidates)
	if len(r.Candidates) > 20 {
		r.Candidates = r.Candidates[:20]
	}
	groups := append(r.FlightGroups, b.FlightGroups...)
	sortCandidates(groups)
	r.FlightGroups = nil
	seen := map[string]bool{}
	for _, g := range groups {
		key := flightKey(g)
		if !seen[key] && len(r.FlightGroups) < 20 {
			r.FlightGroups = append(r.FlightGroups, g)
			seen[key] = true
		}
	}
	r.Hypotheses = followupSeeds(append(r.Hypotheses, b.Hypotheses...), 500, 5, 256)
	sort.SliceStable(r.ScoreGrid, func(i, j int) bool {
		a, b := r.ScoreGrid[i], r.ScoreGrid[j]
		if a.Lat != b.Lat {
			return a.Lat < b.Lat
		}
		return a.Lon < b.Lon
	})
	r.Status = "no_match"
	if len(r.Candidates) > 0 {
		r.Status = "candidate_matches"
	}
	if !strings.Contains(r.Note, "Tiled search") {
		r.Note += " Tiled search covers the entire original region at the requested spacing; counts include any repeated tile-boundary evaluations. Each tile refines its joint survivors independently."
	}
	r.Ambiguity = ""
	AssessAmbiguity(&r)
	return r
}
