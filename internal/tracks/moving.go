package tracks

import (
	"math"
	"sort"

	"pinpoint/internal/geo"
)

// ReceivedMoving evaluates a receiver's position and velocity AT ARRIVAL.
// Sound still originates at the retarded source position. In still air, with
// n pointing from receiver to source, f/f_emitted=(c+n.v_receiver)/(c+n.v_source).
// Treating receiver motion as merely subtracting velocities in the denominator
// is incorrect. The caller must use one consistent receiver path across events.
// It returns emission time as well, for models of changing source pitch.
func (t Track) ReceivedMoving(receiver, velocity geo.Vec, arrival, sound, maxGap float64) (factor, distance, emitted float64, ok bool) {
	for _, v := range []float64{receiver.X, receiver.Y, receiver.Z, velocity.X, velocity.Y, velocity.Z, arrival, sound, maxGap} {
		if !geo.Finite(v) {
			return 0, 0, 0, false
		}
	}
	if sound <= 0 || maxGap <= 0 || velocity.Norm() >= sound {
		return 0, 0, 0, false
	}
	f, d, yes := t.Received(receiver, arrival, sound, maxGap)
	if !yes {
		return 0, 0, 0, false
	}
	emission := arrival - d/sound
	if velocity == (geo.Vec{}) {
		return f, d, emission, true
	}
	i := sort.Search(len(t.Points), func(i int) bool { return t.Points[i].Time >= emission })
	if i == 0 || i == len(t.Points) {
		return 0, 0, 0, false
	}
	a, b := t.Points[i-1], t.Points[i]
	span := b.Time - a.Time
	if span <= 0 || span > maxGap {
		return 0, 0, 0, false
	}
	pos := a.Position.Add(b.Position.Sub(a.Position).Scale((emission - a.Time) / span))
	n := pos.Sub(receiver).Scale(1 / d)
	f *= 1 + n.Dot(velocity)/sound
	if !geo.Finite(f) || f <= 0 || math.Abs(n.Norm()-1) > 1e-3 {
		return 0, 0, 0, false
	}
	return f, d, emission, true
}
