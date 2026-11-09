package search

import (
	"context"
	"math"

	"pinpoint/internal/acoustic"
)

// guidedLinear fits a competing straight frequency ridge on training frames.
// Strongest in-band training peaks initialize it; reserved power cannot change
// its intercept, slope or which frames are evaluated. It is an explanation of
// the same band, not a claim that such a source actually exists.
func guidedLinear(ctx context.Context, s *acoustic.Spectrum, from, to float64, band [2]float64, foldOrigin *float64) GuidedFit {
	var result GuidedFit
	window := float64(s.Window) / float64(s.SampleRate)
	block, guard := math.Max(10, 4*window), window/2
	ref := (from + to) / 2
	df := float64(s.SampleRate) / float64(s.Window)
	first := max(0, int(math.Ceil((band[0]-s.Hz(0))/df)))
	last := min(len(s.Rows[0])-1, int(math.Floor((band[1]-s.Hz(0))/df)))
	type sample struct {
		frame int
		sec   float64
		test  bool
	}
	var points []sample
	n, sx, sy, sxx, sxy := 0., 0., 0., 0., 0.
	for sec := from; sec <= to; sec += math.Max(1, s.Step()) {
		if ctx.Err() != nil {
			return result
		}
		frame := int(math.Round((sec - s.Time(0)) / s.Step()))
		if frame < 0 || frame >= len(s.Rows) {
			continue
		}
		actual := s.Time(frame)
		origin := from
		if foldOrigin != nil {
			origin = *foldOrigin
		}
		phase := math.Mod(actual-origin, block)
		if phase < 0 {
			phase += block
		}
		if phase < guard || phase >= block-guard {
			continue
		}
		test := int(math.Floor((actual-origin)/block))%2 == 1
		points = append(points, sample{frame, actual, test})
		if test {
			continue
		}
		peak := math.Inf(-1)
		hz := 0.
		for bin := first; bin <= last; bin++ {
			v, ok := guidedContrast(s, frame, s.Hz(bin))
			if ok && v > peak {
				peak = v
				hz = s.Hz(bin)
			}
		}
		if math.IsInf(peak, -1) {
			return result
		}
		u := actual - ref
		n++
		sx += u
		sy += hz
		sxx += u * u
		sxy += u * hz
	}
	den := n*sxx - sx*sx
	if n < 8 || den <= 0 {
		return result
	}
	slope := (n*sxy - sx*sy) / den
	intercept := (sy - slope*sx) / n
	sumTrain, sumTest, ntest, positive := 0., 0., 0, 0
	for _, q := range points {
		v, ok := guidedContrast(s, q.frame, intercept+slope*(q.sec-ref))
		if !ok {
			return result
		}
		if q.test {
			sumTest += v
			ntest++
			if v > 0 {
				positive++
			}
		} else {
			sumTrain += v
		}
	}
	if ntest < 8 {
		return result
	}
	result = GuidedFit{F0: intercept, Train: sumTrain / n, Heldout: sumTest / float64(ntest), Positive: float64(positive) / float64(ntest), Coverage: 1, Valid: true, Selection: sumTrain / n}
	result.Slope = slope
	result.Reference = ref
	return result
}
