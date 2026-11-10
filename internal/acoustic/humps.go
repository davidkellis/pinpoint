package acoustic

import "sort"

// A Hump is a broadband loudness rise lasting several seconds: the signature of
// an aircraft pass even when it leaves no tonal curve (jets usually do not).
// Its peak time estimates when sound from the closest approach arrived.
type Hump struct {
	Time  float64 `json:"peak_audio_s"`
	Rise  float64 `json:"rise_db"`
	Width float64 `json:"width_s"`
}

// HumpOptions: the level is smoothed with a running median over Smooth
// seconds, compared with a running Percentile of the level over +/-Baseline
// seconds, and peaks at least MinRise dB above that baseline whose half-rise
// width lies within MinWidth..MaxWidth seconds are kept. Peaks closer than
// Merge seconds keep only the larger.
type HumpOptions struct {
	Smooth, Baseline, Percentile, MinRise, MinWidth, MaxWidth, Merge float64
}

func DefaultHumpOptions() HumpOptions {
	return HumpOptions{Smooth: 5, Baseline: 90, Percentile: .25, MinRise: 4, MinWidth: 8, MaxWidth: 150, Merge: 25}
}

func runningStat(v []float64, half int, q float64) []float64 {
	out := make([]float64, len(v))
	w := make([]float64, 0, 2*half+1)
	for i := range v {
		w = w[:0]
		for j := max(0, i-half); j <= min(len(v)-1, i+half); j++ {
			w = append(w, v[j])
		}
		sort.Float64s(w)
		out[i] = w[int(q*float64(len(w)-1)+.5)]
	}
	return out
}

// FindHumps lists loudness humps in time order.
func FindHumps(l Loudness, o HumpOptions) []Hump {
	if len(l.Level) == 0 || !(l.Step > 0) {
		return nil
	}
	step := l.Step
	smooth := runningStat(l.Level, int(o.Smooth/2/step+.5), .5)
	base := runningStat(smooth, int(o.Baseline/step+.5), o.Percentile)
	excess := make([]float64, len(smooth))
	for i := range smooth {
		excess[i] = smooth[i] - base[i]
	}
	var hs []Hump
	lastEnd := -1
	for i := 1; i+1 < len(excess); i++ {
		e := excess[i]
		if e < o.MinRise || e < excess[i-1] || e < excess[i+1] {
			continue
		}
		a, b := i, i
		for a > 0 && excess[a-1] >= e/2 {
			a--
		}
		for b+1 < len(excess) && excess[b+1] >= e/2 {
			b++
		}
		width := float64(b-a+1) * step
		if a == 0 || b == len(excess)-1 || width < o.MinWidth || width > o.MaxWidth {
			continue
		}
		h := Hump{Time: (float64(i) + .5) * step, Rise: e, Width: width}
		// A flat top has many equal maxima; one rise is one hump.
		if n := len(hs); n > 0 && (a <= lastEnd || h.Time-hs[n-1].Time < o.Merge) {
			lastEnd = max(lastEnd, b)
			if h.Rise > hs[n-1].Rise {
				hs[n-1] = h
			}
			continue
		}
		hs = append(hs, h)
		lastEnd = b
	}
	return hs
}
