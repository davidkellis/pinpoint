package search

import (
	"math"
	"sort"

	"pinpoint/internal/acoustic"
	"pinpoint/internal/geo"
	"pinpoint/internal/tracks"
)

// "Silence is a signal": at the true receiver, aircraft predicted to pass close
// by should make the recording louder at their predicted arrival times, and the
// closer the pass the larger the rise. At a coincidental location, predicted
// close passes often coincide with quiet audio. LoudnessCheck measures that
// agreement without needing any tonal curve. It is heuristic evidence: aircraft
// loudness varies with type, power setting, shielding and indoor recording.

type LoudnessPass struct {
	Track   string  `json:"track_id"`
	Slant   float64 `json:"closest_slant_m"`
	Arrival float64 `json:"arrival_audio_s"`
	Rise    float64 `json:"rise_db"`
}

type LoudnessCheck struct {
	Passes     int            `json:"passes"`
	Violations int            `json:"close_but_quiet"`
	Spearman   float64        `json:"spearman_proximity_rise"`
	Z          float64        `json:"z"`
	Detail     []LoudnessPass `json:"detail,omitempty"`
}

// LoudnessOptions: passes closer than Radius are compared; a pass closer than
// CloseSlant whose rise is below QuietRise counts as a violation.
type LoudnessOptions struct {
	Radius, CloseSlant, QuietRise, Half, Context, SoundSpeed, MaxGap float64
	Detail                                                           bool
}

func DefaultLoudnessOptions() LoudnessOptions {
	return LoudnessOptions{Radius: 4000, CloseSlant: 1500, QuietRise: 1, Half: 10, Context: 90, SoundSpeed: 343, MaxGap: 30}
}

// closePasses returns each continuous excursion of a track within radius of
// obs, with its closest slant distance and the audio time its sound arrives.
// epoch is the absolute time of audio second zero (start plus clock).
func closePasses(t tracks.Track, obs geo.Vec, epoch float64, o LoudnessOptions) []LoudnessPass {
	var out []LoudnessPass
	cur := LoudnessPass{Track: t.ID, Slant: math.Inf(1)}
	flush := func() {
		if !math.IsInf(cur.Slant, 1) {
			out = append(out, cur)
		}
		cur = LoudnessPass{Track: t.ID, Slant: math.Inf(1)}
	}
	for i := 1; i < len(t.Points); i++ {
		a, b := t.Points[i-1], t.Points[i]
		dt := b.Time - a.Time
		if dt <= 0 || dt > o.MaxGap {
			flush()
			continue
		}
		d := b.Position.Sub(a.Position)
		r := a.Position.Sub(obs)
		u := 0.
		if vv := d.Dot(d); vv > 0 {
			u = math.Max(0, math.Min(1, -r.Dot(d)/vv))
		}
		slant := r.Add(d.Scale(u)).Norm()
		if slant >= o.Radius {
			flush()
			continue
		}
		if slant < cur.Slant {
			cur.Slant = slant
			cur.Arrival = a.Time + u*dt + slant/o.SoundSpeed - epoch
		}
	}
	flush()
	return out
}

// CheckLoudness compares predicted close passes with measured loudness rises.
func CheckLoudness(l acoustic.Loudness, ts []tracks.Track, obs geo.Vec, epoch float64, o LoudnessOptions) LoudnessCheck {
	var c LoudnessCheck
	duration := float64(len(l.Level)) * l.Step
	var prox, rise []float64
	for _, t := range ts {
		for _, p := range closePasses(t, obs, epoch, o) {
			if p.Arrival < o.Context || p.Arrival > duration-o.Context {
				continue
			}
			v, ok := l.Rise(p.Arrival, o.Half, o.Context)
			if !ok {
				continue
			}
			p.Rise = v
			c.Passes++
			prox = append(prox, -math.Log10(math.Max(p.Slant, 1)))
			rise = append(rise, v)
			if p.Slant < o.CloseSlant && v < o.QuietRise {
				c.Violations++
			}
			if o.Detail {
				c.Detail = append(c.Detail, p)
			}
		}
	}
	if c.Passes >= 5 {
		c.Spearman = spearman(prox, rise)
		c.Z = c.Spearman * math.Sqrt(float64(c.Passes-1))
	}
	return c
}

func averageRanks(v []float64) []float64 {
	idx := make([]int, len(v))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool { return v[idx[a]] < v[idx[b]] })
	r := make([]float64, len(v))
	for i := 0; i < len(idx); {
		j := i
		for j+1 < len(idx) && v[idx[j+1]] == v[idx[i]] {
			j++
		}
		for k := i; k <= j; k++ {
			r[idx[k]] = float64(i+j) / 2
		}
		i = j + 1
	}
	return r
}

func spearman(x, y []float64) float64 {
	a, b := averageRanks(x), averageRanks(y)
	n := float64(len(a))
	ma, mb := (n-1)/2, (n-1)/2
	sab, saa, sbb := 0., 0., 0.
	for i := range a {
		sab += (a[i] - ma) * (b[i] - mb)
		saa += (a[i] - ma) * (a[i] - ma)
		sbb += (b[i] - mb) * (b[i] - mb)
	}
	if saa == 0 || sbb == 0 {
		return 0
	}
	return sab / math.Sqrt(saa*sbb)
}

// Contradicts reports whether predicted close passes are mostly unheard: the
// proximity/loudness rank correlation is negative, or at least max(3, 25%) of
// the passes are close but quiet. Fewer than five passes never contradict.
func (c LoudnessCheck) Contradicts() bool {
	if c.Passes < 5 {
		return false
	}
	return c.Spearman < 0 || c.Violations >= max(3, (c.Passes+3)/4)
}
