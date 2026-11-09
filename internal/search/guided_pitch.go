package search

import (
	"context"
	"math"

	"pinpoint/internal/acoustic"
)

// An immutable range-maximum index avoids repeating a full stationary-pitch
// scan for every curved candidate. Geometry sets the queried frequency band;
// only the disjoint training frames supply its scores.
type guidedStationaryScores struct {
	start, step float64
	n           int
	tree        []float64
}

func guidedStationary(ctx context.Context, s *acoustic.Spectrum, points []guidedSample, lo, hi, step float64) guidedStationaryScores {
	lo = math.Max(s.Hz(0), lo)
	hi = math.Min(s.Hz(len(s.Rows[0])-1), hi)
	step = math.Max(step, (hi-lo)/20000)
	r := guidedStationaryScores{start: lo, step: step}
	if hi < lo {
		return r
	}
	count := int(math.Floor((hi-lo)/step)) + 1
	r.n = 1
	for r.n < count {
		r.n *= 2
	}
	r.tree = make([]float64, 2*r.n)
	for i := range r.tree {
		r.tree[i] = math.Inf(-1)
	}
	for k := 0; k < count; k++ {
		if ctx.Err() != nil {
			return guidedStationaryScores{}
		}
		f := lo + float64(k)*step
		sum, n := 0., 0
		valid := true
		for _, q := range points {
			if q.test {
				continue
			}
			v, ok := guidedContrast(s, q.frame, f)
			if !ok {
				valid = false
				break
			}
			sum += v
			n++
		}
		if valid && n >= 8 {
			r.tree[r.n+k] = sum / float64(n)
		}
	}
	for i := r.n - 1; i > 0; i-- {
		r.tree[i] = math.Max(r.tree[2*i], r.tree[2*i+1])
	}
	return r
}

func (r guidedStationaryScores) maximum(lo, hi float64) (float64, bool) {
	if r.n == 0 {
		return 0, false
	}
	// Include neighboring grid bins conservatively, rather than missing a hum
	// on the edge because this candidate's band has another fractional origin.
	a := max(0, int(math.Floor((lo-r.start)/r.step))) + r.n
	b := min(r.n, int(math.Ceil((hi-r.start)/r.step))+1) + r.n
	v := math.Inf(-1)
	for a < b {
		if a%2 == 1 {
			v = math.Max(v, r.tree[a])
			a++
		}
		if b%2 == 1 {
			b--
			v = math.Max(v, r.tree[b])
		}
		a /= 2
		b /= 2
	}
	return v, !math.IsInf(v, -1)
}
