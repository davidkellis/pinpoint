package acoustic

import (
	"math"
	"sort"
)

type ridgePoint struct {
	frame     int
	hz, value float64
}
type tonalRidge struct {
	points   []ridgePoint
	slope    float64
	strength float64
}

// Discover proposes sustained tonal passes, using local spectral contrast so
// broadband wind and speech do not become long chains of arbitrary peaks.
// A proposal is evidence to test against trajectories, not an aircraft ID.
const DetectorVersion = "whole-recording-v9"

type DetectionWindow struct {
	Start      float64 `json:"start_s"`
	End        float64 `json:"end_s"`
	Candidates int     `json:"candidates"`
}
type DetectionReport struct {
	Strong           int               `json:"strong_curve_count"`
	Review           int               `json:"review_curve_count"`
	Joint            int               `json:"joint_curve_count"`
	Version          string            `json:"version"`
	ScannedStart     float64           `json:"scanned_start_s"`
	ScannedEnd       float64           `json:"scanned_end_s"`
	CandidateSeconds float64           `json:"candidate_seconds"`
	Windows          []DetectionWindow `json:"windows"`
	Events           []Event           `json:"events"`
	Rejected         map[string]int    `json:"rejected"`
}

func (s *Spectrum) Discover() []Event { return s.DiscoverAll().Events }
func (s *Spectrum) DiscoverAll() DetectionReport {
	report := DetectionReport{Version: DetectorVersion, Events: []Event{}, Rejected: map[string]int{}}
	policy := s.settings()
	if policy.short {
		report.Version = ShortPassVersion
	}
	if len(s.Rows) == 0 {
		return report
	}
	halfWindow := float64(s.Window) / 2 / float64(s.SampleRate)
	report.ScannedStart = math.Max(0, s.Time(0)-halfWindow)
	report.ScannedEnd = s.Time(len(s.Rows)-1) + halfWindow

	var active, done []tonalRidge
	df := float64(s.SampleRate) / float64(s.Window)
	minBand := max(3, int(12/df))
	for frame, row := range s.Rows {
		var peaks []ridgePoint
		for k := minBand; k < len(row)-minBand; k++ {
			if row[k] < 6 || row[k] <= row[k-1] || row[k] < row[k+1] {
				continue
			}
			// Scale the neighborhood with frequency: high-frequency fan tones
			// sweep farther within one FFT. Sample sidebands, not the peak skirt.
			band := max(minBand, int(s.Hz(k)*.025/df))
			if k < band || k+band >= len(row) {
				continue
			}
			background := make([]float64, 0, 10)
			for _, sign := range []int{-1, 1} {
				for n := 0; n < 5; n++ {
					background = append(background, float64(row[k+sign*(band/2+n*band/8)]))
				}
			}
			if float64(row[k])-median(background) < 6 {
				continue
			}
			peaks = append(peaks, ridgePoint{frame, s.Hz(k), float64(row[k])})
		}
		sort.Slice(peaks, func(i, j int) bool { return peaks[i].value > peaks[j].value })
		// Separate budgets prevent high-frequency clutter crowding out the
		// low-frequency band. These cap peaks per frame, not detected events.
		kept := peaks[:0]
		counts := make([]int, max(3, int(s.Hz(len(row)-1)/600)+1))
		for _, p := range peaks {
			band := int(p.hz / 600)
			if counts[band] < 80 {
				kept = append(kept, p)
				counts[band]++
			}
		}
		peaks = kept
		used := make([]bool, len(peaks))
		// A long background tone must not steal a brighter flyover at a
		// frequency crossing merely because it started earlier.
		for i := range active {
			r := &active[i]
			from := max(0, len(r.points)-12)
			v := 0.0
			for _, p := range r.points[from:] {
				v += math.Min(40, p.value)
			}
			r.strength = v / float64(len(r.points)-from) * math.Min(1, float64(len(r.points))/6)
		}
		sort.SliceStable(active, func(i, j int) bool { return active[i].strength > active[j].strength })
		for i := range active {
			r := &active[i]
			last := r.points[len(r.points)-1]
			dt := float64(frame-last.frame) * s.Step()
			expected := last.hz + r.slope*dt
			best := -1
			scale := math.Max(1, last.hz/200)
			dist := math.Max(1, 2*dt*scale)
			quantization, down, up := .6, 8*scale, scale
			if policy.short {
				// A short FFT has wider bins: the old one-Hz linking radius
				// could never follow a tone moving by even one frequency bin.
				quantization = 1.5 * df
				down, up = math.Max(8*scale, .2*last.hz), math.Max(scale, .02*last.hz)
				dist = math.Max(quantization, .1*last.hz*dt)
			}
			for k, p := range peaks {
				d := p.hz - last.hz
				if !used[k] && d >= -down*dt-quantization && d <= up*dt+quantization && math.Abs(p.hz-expected) <= dist {
					best = k
					dist = math.Abs(p.hz - expected)
				}
			}
			if best >= 0 {
				p := peaks[best]
				used[best] = true
				r.slope = .7*r.slope + .3*(p.hz-last.hz)/dt
				r.points = append(r.points, p)
			}
		}
		for k, p := range peaks {
			if !used[k] {
				active = append(active, tonalRidge{points: []ridgePoint{p}})
			}
		}
		keep := active[:0]
		for _, r := range active {
			if float64(frame-r.points[len(r.points)-1].frame)*s.Step() > policy.gap {
				if len(r.points) > 10 {
					done = append(done, r)
				}
			} else {
				keep = append(keep, r)
			}
		}
		active = keep
	}
	done = append(done, active...)
	type proposal struct {
		event    Event
		strength float64
	}
	var candidates []proposal
	var chunks []tonalRidge
	for _, r := range done {
		if len(r.points) == 0 {
			continue
		}
		for from := 0; from < len(r.points); {
			until := from
			for until < len(r.points) && s.Time(r.points[until].frame)-s.Time(r.points[from].frame) <= 120 {
				until++
			}
			chunks = append(chunks, tonalRidge{points: r.points[from:until]})
			if until == len(r.points) {
				break
			}
			next := from + 1
			for next < until && s.Time(r.points[next].frame)-s.Time(r.points[from].frame) < 100 {
				next++
			}
			from = next
		}
	}
	for _, r := range chunks {
		p, q := r.points[0], r.points[len(r.points)-1]
		duration := float64(q.frame-p.frame) * s.Step()
		if duration < policy.fragment || p.hz-q.hz < math.Max(2, p.hz*.02) || float64(len(r.points))/float64(q.frame-p.frame+1) < .65 {
			report.Rejected["duration_drop_or_coverage"]++
			continue
		}
		measurement := fitRidge(s, r.points)
		if measurement.ShapePass {
			traced := retraceRidge(s, r.points, measurement)
			refitted := fitRidge(s, traced)
			// Keep the original path if retracing switches to an incompatible
			// tone or degrades its already coherent model.
			if refitted.ShapePass && refitted.RMSE <= math.Max(measurement.RMSE*1.5, float64(s.SampleRate)/float64(s.Window)) {
				r.points, measurement = traced, refitted
				p, q = r.points[0], r.points[len(r.points)-1]
			}
		}
		f0, error := measurement.F0, measurement.RMSE
		if error > math.Max(1.5, f0*.015) {
			report.Rejected["incoherent_frequency_curve"]++
			continue
		}
		sum := 0.0
		for _, p := range r.points {
			sum += math.Min(30, p.value)
		}
		e := eventForRidge(s, r.points, measurement)
		candidates = append(candidates, proposal{e, sum / float64(len(r.points)) * math.Sqrt(duration) / (1 + error)})
	}
	sort.Slice(candidates, func(i, j int) bool {
		a, b := evidenceRank(candidates[i].event), evidenceRank(candidates[j].event)
		if a != b {
			return a > b
		}
		return candidates[i].strength > candidates[j].strength
	})
	out := []Event{}
	for _, p := range candidates {
		duplicate := false
		for _, e := range out {
			overlap := math.Min(e.End, p.event.End) - math.Max(e.Start, p.event.Start)
			if overlap < .65*math.Min(e.End-e.Start, p.event.End-p.event.Start) {
				continue
			}
			// Only collapse simultaneous copies of the same ridge or its harmonics.
			// Concurrent aircraft at different frequencies remain separate events.
			if sameRidge(e, p.event) {
				duplicate = true
				break
			}
		}
		if duplicate {
			report.Rejected["duplicate_ridge_or_harmonic"]++
			continue
		}
		out = append(out, p.event)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Start < out[j].Start })
	report.SetEvents(out)
	return report
}

// SetEvents recomputes interval coverage after selecting reviewed/demo events.
func (report *DetectionReport) SetEvents(out []Event) {
	report.Strong, report.Review, report.Joint = 0, 0, 0
	for _, e := range out {
		if e.Evidence != nil && e.Evidence.Level == "review" {
			report.Review++
		}
		if e.Evidence != nil && e.Evidence.Level == "strong" {
			report.Strong++
			if e.Measurement != nil && e.Measurement.Usable {
				report.Joint++
			}
		}
	}
	report.Events = out
	report.Windows = nil
	report.CandidateSeconds = 0
	for from := report.ScannedStart; from < report.ScannedEnd; from += 60 {
		w := DetectionWindow{Start: from, End: math.Min(from+60, report.ScannedEnd)}
		for _, e := range out {
			if e.Start < w.End && e.End > w.Start {
				w.Candidates++
			}
		}
		report.Windows = append(report.Windows, w)
	}
	end := report.ScannedStart
	for _, e := range out {
		report.CandidateSeconds += math.Max(0, e.End-math.Max(e.Start, end))
		end = math.Max(end, e.End)
	}
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// Broad, constant-speed fits only screen ridge coherence. Final scoring still
// uses measured 3D flight trajectories and retarded emission time.
func fitRidge(s *Spectrum, points []ridgePoint) RidgeMeasurement {
	a, b := s.Time(points[0].frame), s.Time(points[len(points)-1].frame)
	stride := max(1, len(points)/60)
	var trials []ridgeTrial
	fit := func(v, d, t0 float64) (float64, float64) {
		if v < 15 || v > 300 || d < 100 || d > 15000 {
			return 0, math.Inf(1)
		}
		type sample struct{ factor, hz float64 }
		samples := []sample{}
		xy, xx := 0.0, 0.0
		for i := 0; i < len(points); i += stride {
			p := points[i]
			tr := s.Time(p.frame) - t0 + d/343
			te := (343*343*tr - math.Sqrt(343*343*v*v*tr*tr+(343*343-v*v)*d*d)) / (343*343 - v*v)
			f := 1 / (1 + v*v*te/math.Hypot(d, v*te)/343)
			samples = append(samples, sample{f, p.hz})
			xy += f * p.hz
			xx += f * f
		}
		f0 := xy / xx
		err := 0.0
		for _, p := range samples {
			delta := f0*p.factor - p.hz
			err += delta * delta
		}
		rmse := math.Sqrt(err / float64(len(samples)))
		trials = append(trials, ridgeTrial{v, d, t0, f0, rmse})
		return f0, rmse
	}
	best, freq, cpa, bv, bd := math.Inf(1), 0.0, (a+b)/2, 80.0, 2000.0
	for v := 30.0; v <= 290; v += 40 {
		for d := 300.0; d <= 10000; d *= 1.7 {
			for k := -3; k <= 9; k++ {
				t0 := a + (b-a)*float64(k)/6
				f, e := fit(v, d, t0)
				if e < best {
					best, freq, cpa, bv, bd = e, f, t0, v, d
				}
			}
		}
	}
	// Several coarse seeds avoid a local minimum when data are missing or
	// held out. Keep stepping at each scale before shrinking the mesh.
	seeds := append([]ridgeTrial(nil), trials...)
	sort.Slice(seeds, func(i, j int) bool { return seeds[i].rmse < seeds[j].rmse })
	for _, seed := range seeds[:min(4, len(seeds))] {
		local := seed
		dv, dd, dt := 20.0, seed.distance*.3, (b-a)/12
		for round := 0; round < 9; round++ {
			for step := 0; step < 6; step++ {
				before := local
				for _, v := range []float64{before.speed - dv, before.speed, before.speed + dv} {
					for _, d := range []float64{before.distance - dd, before.distance, before.distance + dd} {
						for _, t0 := range []float64{before.cpa - dt, before.cpa, before.cpa + dt} {
							f, e := fit(v, d, t0)
							if e < local.rmse {
								local = ridgeTrial{v, d, t0, f, e}
							}
						}
					}
				}
				if local.rmse >= before.rmse {
					break
				}
			}
			dv /= 2
			dd /= 2
			dt /= 2
		}
		if local.rmse < best {
			best, freq, cpa, bv, bd = local.rmse, local.f0, local.cpa, local.speed, local.distance
		}
	}
	return measureFit(s, points, ridgeTrial{bv, bd, cpa, freq, best}, trials)
}

// RidgeHz interpolates the measured ridge without extrapolating through long
// gaps. It keeps trajectory scoring tied to the actual detected sound.
func (e Event) RidgeHz(t float64) (float64, bool) {
	if len(e.Ridge) == 0 {
		return 0, false
	}
	i := sort.Search(len(e.Ridge), func(i int) bool { return e.Ridge[i].Time >= t })
	if i < len(e.Ridge) && e.Ridge[i].Time == t {
		return e.Ridge[i].Hz, true
	}
	if i == 0 || i == len(e.Ridge) {
		return 0, false
	}
	a, b := e.Ridge[i-1], e.Ridge[i]
	if b.Time-a.Time > 4 {
		return 0, false
	}
	u := (t - a.Time) / (b.Time - a.Time)
	return a.Hz + (b.Hz-a.Hz)*u, true
}

// Compare a harmonic ratio throughout the overlap. Two independent ridges can
// cross at one frequency, which is not sufficient reason to discard either.
func sameRidge(a, b Event) bool {
	from, until := math.Max(a.Start, b.Start), math.Min(a.End, b.End)
	harmonic, count := 0.0, 0
	for k := 0; k < 7; k++ {
		t := from + (until-from)*float64(k+1)/8
		x, okX := a.RidgeHz(t)
		y, okY := b.RidgeHz(t)
		if !okX || !okY {
			continue
		}
		ratio := x / y
		if ratio < 1 {
			ratio = 1 / ratio
		}
		n := math.Round(ratio)
		if n < 1 || n > 5 || math.Abs(ratio-n) > .015*n || count > 0 && n != harmonic {
			return false
		}
		harmonic = n
		count++
	}
	return count >= 5
}
