package acoustic

import (
	"fmt"
	"math"
	"sort"
)

type RidgeSampling struct {
	Input     int         `json:"input_ridge_samples"`
	Measured  int         `json:"measured_samples"`
	Multiple  int         `json:"samples_with_multiple_harmonics"`
	Harmonics map[int]int `json:"measured_harmonic_counts"`
	Note      string      `json:"note"`
}

// MeasureRidgeSamples measures local spectral maxima along an audio-selected
// ridge. Higher harmonics improve frequency precision only when their measured
// peaks agree with the reference tone; a missing peak is never interpolated.
// No ideal Doppler fit, telemetry, receiver or desired location enters sampling.
// This changes observations and clears inherited quality claims: a caller must
// evaluate the resulting evidence afresh. Harmonic settings are experiments,
// not a classifier that can establish an aircraft's identity.
func MeasureRidgeSamples(s *Spectrum, e Event, maximum int) (Event, RidgeSampling, error) {
	r := RidgeSampling{Input: len(e.Ridge), Harmonics: map[int]int{}, Note: "Measured local peaks only; no inherited strong/ideal-fit confidence, no flight or receiver input. Harmonic components are correlated measurements, not distinct aircraft."}
	if s == nil || len(s.Rows) == 0 || s.SampleRate <= 0 || s.Window <= 0 || s.Hop <= 0 || len(s.Rows[0]) < 3 || maximum < 1 || maximum > 8 || len(e.Ridge) < 8 {
		return e, r, fmt.Errorf("invalid ridge sampling inputs")
	}
	df := float64(s.SampleRate) / float64(s.Window)
	out := e
	out.Ridge = nil
	out.Measurement = nil
	out.Evidence = nil
	last := math.Inf(-1)
	for _, p := range e.Ridge {
		if !finite(p.Time) || !finite(p.Hz) || p.Hz <= 0 || p.Time <= last {
			return e, r, fmt.Errorf("ridge samples must be finite, positive and ordered")
		}
		last = p.Time
		frame := int(math.Round((p.Time - s.Time(0)) / s.Step()))
		if frame < 0 || frame >= len(s.Rows) || math.Abs(s.Time(frame)-p.Time) > s.Step()*.51 {
			continue
		}
		type peak struct {
			hz, weight float64
			harmonic   int
		}
		var peaks []peak
		for h := 1; h <= maximum; h++ {
			target := p.Hz * float64(h)
			radius := math.Max(2*df, p.Hz*.006*float64(h))
			a := max(1, int(math.Ceil((target-radius)/df))-s.FirstBin)
			b := min(len(s.Rows[frame])-2, int(math.Floor((target+radius)/df))-s.FirstBin)
			best := -1
			distance := math.Inf(1)
			for k := a; k <= b; k++ {
				row := s.Rows[frame]
				if row[k] < 6 || row[k] <= row[k-1] || row[k] < row[k+1] {
					continue
				}
				f := refinedPeakHz(s, frame, k)
				v, ok := ridgeContrast(s, frame, f)
				if !ok || v < 6 {
					continue
				}
				if d := math.Abs(f - target); d < distance {
					best = k
					distance = d
				}
			}
			if best >= 0 {
				peaks = append(peaks, peak{refinedPeakHz(s, frame, best) / float64(h), float64(h * h), h})
			}
		}
		if len(peaks) == 0 {
			continue
		}
		// A median anchor prevents a single unrelated high harmonic from
		// pulling the measured reference tone away from the other peaks.
		frequencies := make([]float64, len(peaks))
		for i, q := range peaks {
			frequencies[i] = q.hz
		}
		sort.Float64s(frequencies)
		median := frequencies[len(frequencies)/2]
		weighted, weight, n := 0., 0., 0
		for _, q := range peaks {
			if math.Abs(q.hz-median) > math.Max(df, p.Hz*.004) {
				continue
			}
			weighted += q.hz * q.weight
			weight += q.weight
			n++
			r.Harmonics[q.harmonic]++
		}
		if n > 0 {
			out.Ridge = append(out.Ridge, RidgeSample{p.Time, weighted / weight})
			r.Measured++
			if n > 1 {
				r.Multiple++
			}
		}
	}
	if r.Measured < 8 || float64(r.Measured) < .8*float64(r.Input) || out.Ridge[0].Time > e.Ridge[0].Time+2*s.Step() || out.Ridge[len(out.Ridge)-1].Time < e.Ridge[len(e.Ridge)-1].Time-2*s.Step() {
		return e, r, fmt.Errorf("insufficient measured support across the complete ridge")
	}
	return out, r, nil
}
