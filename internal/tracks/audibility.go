package tracks

import (
	"math"
	"pinpoint/internal/geo"
)

// ArrivalInterval is a continuous interval of received sound, not emission time.
type ArrivalInterval struct{ Start, End float64 }

// WithinRadius returns the arrival intervals for valid subsonic trajectory
// segments inside a slant-distance sphere. Radius is an explicit audibility
// assumption, not a prediction of loudness. Gaps are never bridged.
func (t Track) WithinRadius(observer geo.Vec, radius, sound, maxGap float64) []ArrivalInterval {
	var out []ArrivalInterval
	for i := 1; i < len(t.Points); i++ {
		a, b := t.Points[i-1], t.Points[i]
		dt := b.Time - a.Time
		if dt <= 0 || dt > maxGap {
			continue
		}
		r, delta := a.Position.Sub(observer), b.Position.Sub(a.Position)
		vv := delta.Dot(delta)
		if vv < dt*dt || vv >= sound*sound*dt*dt {
			continue
		}
		center := -r.Dot(delta) / vv
		closest := r.Add(delta.Scale(center)).Norm()
		if closest >= radius {
			continue
		}
		half := math.Sqrt((radius*radius - closest*closest) / vv)
		lo, hi := math.Max(0, center-half), math.Min(1, center+half)
		if lo >= hi {
			continue
		}
		arrival := func(u float64) float64 { return a.Time + u*dt + r.Add(delta.Scale(u)).Norm()/sound }
		w := ArrivalInterval{arrival(lo), arrival(hi)}
		if len(out) > 0 && w.Start <= out[len(out)-1].End+1e-6 {
			out[len(out)-1].End = math.Max(out[len(out)-1].End, w.End)
		} else {
			out = append(out, w)
		}
	}
	return out
}
