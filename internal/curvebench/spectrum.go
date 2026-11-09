// Package curvebench compares experimental acoustic representations without
// changing production detection thresholds or declaring aircraft identities.
package curvebench

import (
	"context"
	"fmt"
	"math"
	"math/cmplx"
	"sort"

	"pinpoint/internal/acoustic"
)

const Rate = 4096

// transform retains the original time grid. Reassignment is frequency-only:
// omega_hat = omega - Im(STFT(window derivative)/STFT(window)).
// It does not fabricate energy, perform time reassignment, or recover lost audio.
func transform(ctx context.Context, audio []float64, seconds int, reassign bool) (*acoustic.Spectrum, error) {
	n := Rate * seconds
	if seconds != 1 && seconds != 2 && seconds != 4 && seconds != 8 {
		return nil, fmt.Errorf("window must be 1, 2, 4 or 8 seconds")
	}
	hop := n / 8
	if len(audio) < n+hop {
		return nil, fmt.Errorf("audio shorter than analysis window")
	}
	s := &acoustic.Spectrum{Version: 1, SampleRate: Rate, Window: n, Hop: hop, FirstBin: 40 * seconds}
	last := 1800 * seconds
	if ((len(audio)-n)/hop+1)*(last-s.FirstBin+1) > 100_000_000 {
		return nil, fmt.Errorf("spectrum exceeds 100 million cells; use shorter clips")
	}
	window, derivative := make([]float64, n), make([]float64, n)
	for i := range window {
		angle := 2 * math.Pi * float64(i) / float64(n-1)
		window[i] = .5 - .5*math.Cos(angle)
		derivative[i] = math.Pi / float64(n-1) * math.Sin(angle)
	}
	a, b := make([]complex128, n), make([]complex128, n)
	for from := 0; from+n <= len(audio); from += hop {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for i := range a {
			a[i] = complex(audio[from+i]*window[i], 0)
			if reassign {
				b[i] = complex(audio[from+i]*derivative[i], 0)
			}
		}
		acoustic.FFT(a)
		if reassign {
			acoustic.FFT(b)
		}
		power := make([]float64, last-s.FirstBin+1)
		if reassign {
			maxPower := 0.
			for k := 1; k < n/2; k++ {
				p := cmplx.Abs(a[k])
				maxPower = math.Max(maxPower, p*p)
			}
			for k := 1; k < n/2; k++ {
				p := real(a[k])*real(a[k]) + imag(a[k])*imag(a[k])
				if p <= math.Max(1e-20, maxPower*1e-8) {
					continue
				}
				hz := float64(k)*Rate/float64(n) - imag(b[k]/a[k])*Rate/(2*math.Pi)
				x := hz*float64(n)/Rate - float64(s.FirstBin)
				if math.IsNaN(x) || math.IsInf(x, 0) || x < 0 || x > float64(len(power)-1) {
					continue
				}
				j := int(x)
				u := x - float64(j)
				power[j] += p * (1 - u)
				if j+1 < len(power) {
					power[j+1] += p * u
				} else {
					power[j] += p * u
				}
			}
		} else {
			for j := range power {
				v := a[j+s.FirstBin]
				power[j] = real(v)*real(v) + imag(v)*imag(v)
			}
		}
		row := make([]float32, len(power))
		for j, p := range power {
			row[j] = float32(10 * math.Log10(p+1e-20))
		}
		s.Rows = append(s.Rows, row)
	}
	return s, nil
}
func median(v []float64) float64 { sort.Float64s(v); return v[len(v)/2] }
func clone(s *acoustic.Spectrum) *acoustic.Spectrum {
	c := *s
	c.Rows = make([][]float32, len(s.Rows))
	for i, r := range s.Rows {
		c.Rows[i] = append([]float32(nil), r...)
	}
	return &c
}
func whiten(s *acoustic.Spectrum) *acoustic.Spectrum {
	c := clone(s)
	col := make([]float64, len(c.Rows))
	for k := range c.Rows[0] {
		for i := range col {
			col[i] = float64(c.Rows[i][k])
		}
		m := float32(median(col))
		for i := range col {
			c.Rows[i][k] -= m
		}
	}
	row := make([]float64, len(c.Rows[0]))
	for i := range c.Rows {
		for k, v := range c.Rows[i] {
			row[k] = float64(v)
		}
		m := float32(median(row))
		for k := range row {
			c.Rows[i][k] -= m
		}
	}
	return c
}
func localContrast(s *acoustic.Spectrum) *acoustic.Spectrum {
	c := clone(s)
	df := float64(s.SampleRate) / float64(s.Window)
	for i, row := range s.Rows {
		for k, v := range row {
			width := math.Max(12, s.Hz(k)*.025)
			var side [10]float64
			n := 0
			for _, sign := range []float64{-1, 1} {
				for j := 0; j < 5; j++ {
					bin := k + int(math.Round(sign*width*(.5+float64(j)/8)/df))
					if bin >= 0 && bin < len(row) {
						side[n] = float64(row[bin])
						n++
					}
				}
			}
			if n > 0 {
				c.Rows[i][k] = v - float32(median(side[:n]))
			}
		}
	}
	return c
}

// Pool measured harmonic contrast, with declining weights. This can suppress a
// single jet tone as well as reinforce a propeller harmonic family; benchmark
// it separately, never silently replace the single-tone branch.
func harmonicPool(s *acoustic.Spectrum) *acoustic.Spectrum {
	c := clone(s)
	for frame, row := range s.Rows {
		for k, v := range row {
			sum, weight := float64(v), 1.
			for _, h := range []int{2, 3} {
				if x, ok := s.Value(frame, s.Hz(k)*float64(h)); ok {
					w := 1 / float64(h)
					sum += x * w
					weight += w
				}
			}
			c.Rows[frame][k] = float32(sum / weight)
		}
	}
	return c
}
