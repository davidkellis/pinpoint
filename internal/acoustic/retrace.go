package acoustic

import "math"

// retraceRidge reconnects a promising seed using the energy of an entire path,
// rather than giving a crossing peak to whichever greedy track arrived first.
// The ideal fit only defines a broad corridor; frequencies come from the audio.
// Weak/masked frames are omitted rather than invented by interpolation.
func retraceRidge(s *Spectrum, points []ridgePoint, m RidgeMeasurement) []ridgePoint {
	if !m.ShapePass || len(points) < 20 {
		return points
	}
	first, last := points[0].frame, points[len(points)-1].frame
	df := float64(s.SampleRate) / float64(s.Window)
	step := math.Max(df, m.F0*.002)
	radius := math.Max(5, math.Max(m.RMSE*5, m.F0*.07))
	half := int(math.Ceil(radius / step))
	states := half*2 + 1
	frames := last - first + 1
	back := make([]int16, frames*states)
	previous, next := make([]float64, states), make([]float64, states)
	maxMove := max(2, int(math.Ceil(math.Max(1, m.F0*.015)*s.Step()/step)))
	for frame := first; frame <= last; frame++ {
		predicted := idealHz(m, s.Time(frame))
		for k := 0; k < states; k++ {
			hz := predicted + float64(k-half)*step
			contrast, ok := ridgeContrast(s, frame, hz)
			if !ok {
				contrast = -10
			}
			// Clipping stops one transient or loud crossing from outweighing a
			// sustained tone. Reward contrast, not broadband loudness.
			reward := math.Max(-6, math.Min(18, contrast-6))
			best, from := math.Inf(-1), k
			for j := max(0, k-maxMove); j <= min(states-1, k+maxMove); j++ {
				delta := float64(k-j) / float64(maxMove)
				score := previous[j] - 2*delta*delta
				if score > best {
					best, from = score, j
				}
			}
			next[k] = best + reward
			back[(frame-first)*states+k] = int16(from)
		}
		previous, next = next, previous
	}
	end := 0
	for k := range previous {
		if previous[k] > previous[end] {
			end = k
		}
	}
	path := make([]ridgePoint, frames)
	for frame := last; frame >= first; frame-- {
		hz := idealHz(m, s.Time(frame)) + float64(end-half)*step
		// Return the actual spectral maximum near the selected path.
		bin := int(math.Round(hz/df)) - s.FirstBin
		peak, value := bin, math.Inf(-1)
		for k := max(1, bin-int(math.Ceil(step/df))); k <= min(len(s.Rows[frame])-2, bin+int(math.Ceil(step/df))); k++ {
			if float64(s.Rows[frame][k]) > value {
				peak, value = k, float64(s.Rows[frame][k])
			}
		}
		path[frame-first] = ridgePoint{frame, s.Hz(peak), value}
		end = int(back[(frame-first)*states+end])
	}
	var observed []ridgePoint
	for _, p := range path {
		if contrast, ok := ridgeContrast(s, p.frame, p.hz); ok && contrast >= 6 && p.value >= 6 {
			observed = append(observed, p)
		}
	}
	if len(observed) < 20 {
		return points
	}
	return observed
}

func eventForRidge(s *Spectrum, points []ridgePoint, m RidgeMeasurement) Event {
	low, high := points[0].hz, points[0].hz
	for _, p := range points {
		low = math.Min(low, p.hz)
		high = math.Max(high, p.hz)
	}
	start, end := s.Time(points[0].frame), s.Time(points[len(points)-1].frame)
	e := Event{Resolution: float64(s.Window) / float64(s.SampleRate), Measurement: &m, Partial: true, Start: start, End: end, CPATime: math.Max(start, math.Min(end, m.CPA)), FMin: math.Max(1, low*(1-300.0/343)), FMax: high * (1 + 300.0/343), FStep: .5, Harmonics: []int{1}, MinSpeed: 15, MaxSpeed: 300, MaxSlant: 15000}
	for i, p := range points {
		if i%max(1, int(math.Round(s.settings().ridgeStep/s.Step()))) == 0 || i == len(points)-1 {
			e.Ridge = append(e.Ridge, RidgeSample{s.Time(p.frame), p.hz})
		}
	}
	e.Evidence = assessCurve(s, e, points)
	return e
}

// Join overlapping pieces only when measured frequencies agree throughout the
// overlap and the combined measured path passes the evidence checks again.
// This prevents one interrupted flyover becoming two independent constraints.
func joinCurves(s *Spectrum, events []Event) []Event {
	for i := 0; i < len(events); i++ {
		a := events[i]
		if evidenceRank(a) == 0 {
			continue
		}
		for j := i + 1; j < len(events); j++ {
			b := events[j]
			if evidenceRank(b) == 0 {
				continue
			}
			from, until := math.Max(a.Start, b.Start), math.Min(a.End, b.End)
			if until-from < 10 || until-from < .25*math.Min(a.End-a.Start, b.End-b.Start) || a.Start <= b.Start && a.End >= b.End || b.Start <= a.Start && b.End >= a.End || math.Max(a.End, b.End)-math.Min(a.Start, b.Start) > 240 {
				continue
			}
			good := 0
			for k := 1; k <= 7; k++ {
				t := from + (until-from)*float64(k)/8
				x, ok := a.RidgeHz(t)
				y, ok2 := b.RidgeHz(t)
				if ok && ok2 && math.Abs(x-y) <= math.Max(1.5, (x+y)*.0075) {
					good++
				}
			}
			if good < 6 {
				continue
			}
			var points []ridgePoint
			first := max(0, int(math.Ceil((math.Min(a.Start, b.Start)-s.Time(0))/s.Step())))
			last := min(len(s.Rows)-1, int(math.Floor((math.Max(a.End, b.End)-s.Time(0))/s.Step())))
			df := float64(s.SampleRate) / float64(s.Window)
			for frame := first; frame <= last; frame++ {
				best := ridgePoint{value: math.Inf(-1)}
				for _, e := range []Event{a, b} {
					hz, ok := e.RidgeHz(s.Time(frame))
					if !ok {
						continue
					}
					bin := int(math.Round(hz/df)) - s.FirstBin
					for k := max(0, bin-2); k <= min(len(s.Rows[frame])-1, bin+2); k++ {
						value := float64(s.Rows[frame][k])
						if value <= best.value || value < 6 {
							continue
						}
						f := s.Hz(k)
						contrast, ok := ridgeContrast(s, frame, f)
						if ok && contrast >= 6 {
							best = ridgePoint{frame, f, value}
						}
					}
				}
				if !math.IsInf(best.value, -1) {
					points = append(points, best)
				}
			}
			if len(points) < 30 {
				continue
			}
			m := fitRidge(s, points)
			if !m.ShapePass {
				continue
			}
			joined := eventForRidge(s, points, m)
			if a.Evidence.Resolutions >= 2 && b.Evidence.Resolutions >= 2 {
				joined.Evidence.Resolutions = 2
				finishEvidence(joined.Evidence)
			}
			if joined.Evidence.Level != "strong" {
				continue
			}
			events[i] = joined
			events = append(events[:j], events[j+1:]...)
			i--
			break
		}
	}
	return events
}
