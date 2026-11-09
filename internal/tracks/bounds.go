package tracks

import (
	"math"
	"pinpoint/internal/geo"
	"sort"
)

// DopplerBounds encloses gains for every observer inside an ECEF ball and
// every reception time in [from,to]. Endpoint emission times include the ball's
// propagation-delay uncertainty. Missing/invalid geometry returns false so a
// conservative search prefilter can keep the cell instead of rejecting it.
func (t Track) DopplerBounds(center geo.Vec, radius, from, to, sound, gap float64) (float64, float64, bool) {
	_, d0, ok := t.Received(center, from-radius/sound, sound, gap)
	if !ok {
		return 0, 0, false
	}
	_, d1, ok := t.Received(center, to+radius/sound, sound, gap)
	if !ok {
		return 0, 0, false
	}
	low := from - radius/sound - d0/sound - 1e-6
	high := to + radius/sound - d1/sound + 1e-6
	first := sort.Search(len(t.Points), func(i int) bool { return t.Points[i].Time >= low })
	if first == 0 || first == len(t.Points) || high > t.Points[len(t.Points)-1].Time {
		return 0, 0, false
	}
	minRad, maxRad := math.Inf(1), math.Inf(-1)
	for i := first; i < len(t.Points) && t.Points[i-1].Time <= high; i++ {
		a, b := t.Points[i-1], t.Points[i]
		dt := b.Time - a.Time
		if dt <= 0 || dt > gap {
			return 0, 0, false
		}
		v := b.Position.Sub(a.Position).Scale(1 / dt)
		speed := v.Norm()
		if speed >= sound {
			return 0, 0, false
		}
		for _, at := range []float64{math.Max(low, a.Time), math.Min(high, b.Time)} {
			r := a.Position.Add(v.Scale(at - a.Time)).Sub(center)
			d := r.Norm()
			lo, hi := -speed, speed
			if d > radius {
				rad := r.Dot(v) / d
				perp := math.Sqrt(math.Max(0, speed*speed-rad*rad))
				sin := radius / d
				cos := math.Sqrt(math.Max(0, 1-sin*sin))
				lo = rad*cos - perp*sin
				hi = rad*cos + perp*sin
				if rad <= -speed*cos {
					lo = -speed
				}
				if rad >= speed*cos {
					hi = speed
				}
			}
			minRad = math.Min(minRad, lo)
			maxRad = math.Max(maxRad, hi)
		}
	}
	if math.IsInf(minRad, 1) {
		return 0, 0, false
	}
	return sound / (sound + maxRad), sound / (sound + minRad), true
}
