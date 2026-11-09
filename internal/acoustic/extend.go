package acoustic

import (
	"math"
	"sort"
)

// extendRidge follows measured peaks beyond a predictive seed. The beam keeps
// competing continuations through crossings; neither ADS-B nor an extrapolated
// ideal Doppler curve determines the new frequencies. Missing frames stay
// missing. Full-path evidence checks decide whether to accept the extension.
func extendRidge(s *Spectrum, seed []ridgePoint) []ridgePoint {
	if len(seed) < 20 {
		return seed
	}
	before := extendDirection(s, seed, -1)
	after := extendDirection(s, seed, 1)
	out := make([]ridgePoint, 0, len(before)+len(seed)+len(after))
	for i := len(before) - 1; i >= 0; i-- {
		out = append(out, before[i])
	}
	out = append(out, seed...)
	out = append(out, after...)
	return out
}

type extensionState struct {
	point        ridgePoint
	slope, score float64 // slope is in the direction of traversal, Hz/second
	node         int
}

type extensionNode struct {
	point  ridgePoint
	parent int
}

func extendDirection(s *Spectrum, seed []ridgePoint, direction int) []ridgePoint {
	end, other := seed[len(seed)-1], seed[len(seed)-2]
	if direction < 0 {
		end, other = seed[0], seed[1]
	}
	// Estimate endpoint slope over one second to reduce bin quantization noise.
	for k := 1; k < len(seed); k++ {
		i := len(seed) - 1 - k
		if direction < 0 {
			i = k
		}
		other = seed[i]
		if math.Abs(float64(other.frame-end.frame))*s.Step() >= 1 {
			break
		}
	}
	slope := (end.hz - other.hz) / (math.Abs(float64(end.frame-other.frame)) * s.Step())
	beam := []extensionState{{end, slope, 0, -1}}
	var nodes []extensionNode
	best := extensionState{node: -1}
	df := float64(s.SampleRate) / float64(s.Window)
	limit := int(math.Min(60, math.Max(10, float64(seed[len(seed)-1].frame-seed[0].frame)*s.Step())) / s.Step())
	for n := 1; n <= limit; n++ {
		frame := end.frame + direction*n
		if frame < 0 || frame >= len(s.Rows) {
			break
		}
		var next []extensionState
		for _, old := range beam {
			dt := math.Abs(float64(frame-old.point.frame)) * s.Step()
			if dt > s.settings().gap {
				continue
			}
			expected := old.point.hz + old.slope*dt
			radius := math.Max(2*df, old.point.hz*.02*dt)
			lo := max(1, int(math.Floor((expected-radius)/df))-s.FirstBin)
			hi := min(len(s.Rows[frame])-2, int(math.Ceil((expected+radius)/df))-s.FirstBin)
			for k := lo; k <= hi; k++ {
				v := float64(s.Rows[frame][k])
				if v < 6 || s.Rows[frame][k] <= s.Rows[frame][k-1] || s.Rows[frame][k] < s.Rows[frame][k+1] {
					continue
				}
				hz := s.Hz(k)
				contrast, ok := ridgeContrast(s, frame, hz)
				if !ok || contrast < 6 {
					continue
				}
				forwardSlope := float64(direction) * (hz - old.point.hz) / dt
				if forwardSlope > math.Max(df/dt, hz*.02) || forwardSlope < -math.Max(2*df/dt, hz*.2) {
					continue
				}
				innovation := (hz - expected) / radius
				score := old.score + math.Min(12, contrast-4) - 6*innovation*innovation
				p := ridgePoint{frame, hz, v}
				nodes = append(nodes, extensionNode{p, old.node})
				next = append(next, extensionState{p, .8*old.slope + .2*(hz-old.point.hz)/dt, score, len(nodes) - 1})
			}
			// A gap branch allows brief masking, but does not add a fabricated
			// frequency observation. It must reconnect within the gap limit.
			old.score -= 6
			next = append(next, old)
		}
		if len(next) == 0 {
			break
		}
		sort.SliceStable(next, func(i, j int) bool { return next[i].score > next[j].score })
		beam = beam[:0]
		for _, state := range next {
			duplicate := false
			for _, b := range beam {
				if state.point.frame == b.point.frame && math.Abs(state.point.hz-b.point.hz) < df*.5 && math.Abs(state.slope-b.slope) < df {
					duplicate = true
					break
				}
			}
			if !duplicate {
				beam = append(beam, state)
			}
			if len(beam) == 12 {
				break
			}
		}
		if beam[0].point.frame == frame && beam[0].score > best.score {
			best = beam[0]
		}
	}
	var path []ridgePoint
	for id := best.node; id >= 0; id = nodes[id].parent {
		path = append(path, nodes[id].point)
	}
	for a, b := 0, len(path)-1; a < b; a, b = a+1, b-1 {
		path[a], path[b] = path[b], path[a]
	}
	return path
}

func extendEvent(s *Spectrum, e Event) Event {
	if e.Evidence == nil || !e.Evidence.Predictive || e.Measurement == nil || !e.Measurement.ShapePass {
		return e
	}
	var seed []ridgePoint
	first := max(0, int(math.Ceil((e.Start-s.Time(0))/s.Step())))
	last := min(len(s.Rows)-1, int(math.Floor((e.End-s.Time(0))/s.Step())))
	for frame := first; frame <= last; frame++ {
		if hz, ok := e.RidgeHz(s.Time(frame)); ok {
			df := float64(s.SampleRate) / float64(s.Window)
			bin := int(math.Round(hz/df)) - s.FirstBin
			peak := ridgePoint{value: math.Inf(-1)}
			for k := max(1, bin-1); k <= min(len(s.Rows[frame])-2, bin+1); k++ {
				v := float64(s.Rows[frame][k])
				if v < 6 || v <= peak.value || s.Rows[frame][k] <= s.Rows[frame][k-1] || s.Rows[frame][k] < s.Rows[frame][k+1] {
					continue
				}
				if contrast, ok := ridgeContrast(s, frame, s.Hz(k)); ok && contrast >= 6 {
					peak = ridgePoint{frame, s.Hz(k), v}
				}
			}
			if !math.IsInf(peak.value, -1) {
				seed = append(seed, peak)
			}
		}
	}
	points := extendRidge(s, seed)
	if len(points) <= len(seed) || s.Time(points[len(points)-1].frame)-s.Time(points[0].frame) < 1.1*(e.End-e.Start) {
		return e
	}
	m := fitRidge(s, points)
	if !m.ShapePass {
		return e
	}
	extended := eventForRidge(s, points, m)
	// Cross-resolution evidence on the seed does not certify new portions.
	// The caller must corroborate the extended curve separately.
	if !extended.Evidence.Predictive || e.Measurement.Usable && !m.Usable {
		return e
	}
	return extended
}

func extendCandidates(s *Spectrum, report DetectionReport) DetectionReport {
	all := append([]Event(nil), report.Events...)
	for _, e := range report.Events {
		x := extendEvent(s, e)
		if x.Start < e.Start || x.End > e.End {
			all = append(all, x)
		}
	}
	report.SetEvents(all)
	return report
}
