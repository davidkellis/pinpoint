package tracks

import (
	"math"

	"pinpoint/internal/geo"
)

type trackSegments struct {
	count    int
	velocity []geo.Vec
	speed    []float64
	maxSpeed float64
	ordered  bool
}

// PrepareSegments creates a private, receiver-independent geometry index. It
// does not alter positions, times, gaps or velocities. The returned track and
// its Points must be treated as immutable while used by scoring workers.
// Rebuild after changing geometry; Smooth/Reconstruct return unprepared tracks.
func (t Track) PrepareSegments() Track {
	n := max(0, len(t.Points)-1)
	index := &trackSegments{count: len(t.Points), velocity: make([]geo.Vec, n), speed: make([]float64, n), ordered: true}
	for i := 0; i < n; i++ {
		a, b := t.Points[i], t.Points[i+1]
		dt := b.Time - a.Time
		if !geo.Finite(dt) || dt <= 0 {
			index.ordered = false
			continue
		}
		v := b.Position.Sub(a.Position).Scale(1 / dt)
		speed := v.Norm()
		if !geo.Finite(speed) {
			index.ordered = false
		}
		index.velocity[i], index.speed[i] = v, speed
		index.maxSpeed = math.Max(index.maxSpeed, speed)
	}
	t.segments = index
	return t
}

func (t Track) segmentMotion(i int) (geo.Vec, float64) {
	if t.segments != nil && t.segments.count == len(t.Points) {
		return t.segments.velocity[i], t.segments.speed[i]
	}
	a, b := t.Points[i], t.Points[i+1]
	v := b.Position.Sub(a.Position).Scale(1 / (b.Time - a.Time))
	return v, v.Norm()
}
