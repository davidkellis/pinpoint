package search

import (
	"math"
	"sort"
)

func configuredMatches(c Config, choices [][]Match, threshold float64) []Match {
	if !c.DistinctAircraft && c.ScoreObjective != "weakest" {
		return distinctMatches(choices, threshold)
	}
	converted := make([][]Match, len(choices))
	var levels []float64
	for i, row := range choices {
		for _, m := range row {
			if m.Score < threshold {
				continue
			}
			levels = append(levels, m.Score)
			if c.DistinctAircraft {
				m.Track = guidedAircraftID(m.Track)
			}
			converted[i] = append(converted[i], m)
		}
	}
	if c.ScoreObjective == "weakest" {
		sort.Float64s(levels)
		lo, hi := 0, len(levels)
		for lo < hi {
			mid := (lo + hi) / 2
			if len(distinctMatches(converted, levels[mid])) == len(choices) {
				lo = mid + 1
			} else {
				hi = mid
			}
		}
		if lo == 0 {
			return nil
		}
		threshold = levels[lo-1]
	}
	out := distinctMatches(converted, threshold)
	if c.DistinctAircraft {
		for i, m := range out {
			for _, original := range choices[i] {
				if guidedAircraftID(original.Track) == m.Track && original.F0 == m.F0 && original.Score == m.Score {
					out[i] = original
					break
				}
			}
		}
	}
	return out
}

// Rectangular minimum-cost assignment (shortest augmenting paths). Every event
// must pass independently and receive a different track; there is no four-event
// cap and no factorial enumeration of flight combinations.
func distinctMatches(choices [][]Match, threshold float64) []Match {
	n := len(choices)
	ids := map[string]bool{}
	for _, row := range choices {
		for _, m := range row {
			if m.Score >= threshold {
				ids[m.Track] = true
			}
		}
	}
	var names []string
	for id := range ids {
		names = append(names, id)
	}
	sort.Strings(names)
	m := len(names)
	if n == 0 || m < n {
		return nil
	}
	column := map[string]int{}
	for j, id := range names {
		column[id] = j + 1
	}
	rows := make([]map[int]Match, n)
	for i, row := range choices {
		rows[i] = map[int]Match{}
		for _, match := range row {
			if match.Score < threshold {
				continue
			}
			j := column[match.Track]
			old, ok := rows[i][j]
			if !ok || match.Score > old.Score {
				rows[i][j] = match
			}
		}
		if len(rows[i]) == 0 {
			return nil
		}
	}
	u, v := make([]float64, n+1), make([]float64, m+1)
	p, way := make([]int, m+1), make([]int, m+1)
	for i := 1; i <= n; i++ {
		p[0] = i
		minv, used := make([]float64, m+1), make([]bool, m+1)
		for j := 1; j <= m; j++ {
			minv[j] = math.Inf(1)
		}
		j0 := 0
		for {
			used[j0] = true
			i0, j1, delta := p[j0], 0, math.Inf(1)
			for j := 1; j <= m; j++ {
				if used[j] {
					continue
				}
				if match, ok := rows[i0-1][j]; ok {
					cost := -match.Score - u[i0] - v[j]
					if cost < minv[j] {
						minv[j] = cost
						way[j] = j0
					}
				}
				if minv[j] < delta {
					delta = minv[j]
					j1 = j
				}
			}
			if math.IsInf(delta, 1) {
				return nil
			}
			for j := 0; j <= m; j++ {
				if used[j] {
					u[p[j]] += delta
					v[j] -= delta
				} else {
					minv[j] -= delta
				}
			}
			j0 = j1
			if p[j0] == 0 {
				break
			}
		}
		for {
			j1 := way[j0]
			p[j0] = p[j1]
			j0 = j1
			if j0 == 0 {
				break
			}
		}
	}
	out := make([]Match, n)
	for j := 1; j <= m; j++ {
		if p[j] > 0 {
			out[p[j]-1] = rows[p[j]-1][j]
		}
	}
	return out
}
