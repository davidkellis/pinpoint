package search

import (
	"fmt"
	"math"
	"pinpoint/internal/acoustic"
	"pinpoint/internal/geo"
	"pinpoint/internal/tracks"
	"time"
)

func fixedSequenceChecks(c Config, s *acoustic.Spectrum, ts []tracks.Track, b Candidate, indices []int) ([]SequenceCheck, *float64, bool) {
	t0, _ := time.Parse(time.RFC3339Nano, c.RecordingStart)
	start := float64(t0.Unix()) + float64(t0.Nanosecond())/1e9 + b.ClockOffset
	p := geo.ECEF(b.Lat, b.Lon, b.Height)
	byID := map[string]tracks.Track{}
	for _, t := range ts {
		byID[t.ID] = t
	}
	seen := map[string]bool{}
	checks := make([]SequenceCheck, 0, len(c.Events))
	weakest := math.Inf(1)
	all := true
	for i, e := range c.Events {
		sp := eventSpectrum(c, s, i)
		train := prepareFoldObservations(sp, e, "train")
		test := prepareFoldObservations(sp, e, "validation")
		m := b.Events[i]
		q := SequenceCheck{EventIndex: indices[i], Track: m.Track, F0: m.F0, Train: m.Score, TrainSamples: len(train), TestSamples: len(test), Plan: eventFold(e, sp), Reasons: []Rejection{}}
		reject := func(code, detail string) { q.Reasons = append(q.Reasons, Rejection{Code: code, Detail: detail}) }
		identity := guidedAircraftID(m.Track)
		if seen[identity] {
			reject("duplicate_aircraft", "Another selected curve uses the same aircraft identity.")
		}
		seen[identity] = true
		track, ok := byID[m.Track]
		if !ok {
			reject("missing_telemetry", "The fitted flight track is unavailable.")
			checks = append(checks, q)
			all = false
			continue
		}
		track = track.ForObserver(p, c.SoundSpeed)
		flat, slope, ref, fitOK := trainingPitchBaselines(train)
		if !fitOK || len(test) < 5 {
			reject("insufficient_validation", "Too few disjoint measured samples remain after FFT guards.")
			checks = append(checks, q)
			all = false
			continue
		}
		modelSum, flatSum, lineSum, squared, hzSum := 0., 0., 0., 0., 0.
		modelN, flatN, lineN, ridgeN, positive, frames := 0, 0, 0, 0, 0, 0
		for _, o := range test {
			factor, _, valid := track.Received(p, start+o.time, c.SoundSpeed, c.MaxGap)
			if !valid {
				continue
			}
			predicted := m.F0 * factor
			if o.hasRidge {
				delta := predicted - o.hz
				squared += delta * delta
				hzSum += o.hz
				ridgeN++
			}
			mv, fv, lv, mn, fn, ln := 0., 0., 0., 0, 0, 0
			for _, h := range e.Harmonics {
				if v, yes := sp.Value(o.frame, predicted*float64(h)); yes {
					// Rank raw frame evidence before capping aggregate scores:
					// two bright bins must not become an artificial 30 dB tie.
					mv += v
					v = math.Max(-10, math.Min(30, v))
					modelSum += v
					modelN++
					mn++
				}
				if v, yes := sp.Value(o.frame, flat*float64(h)); yes {
					fv += v
					v = math.Max(-10, math.Min(30, v))
					flatSum += v
					flatN++
					fn++
				}
				if v, yes := sp.Value(o.frame, (flat+slope*(o.time-ref))*float64(h)); yes {
					lv += v
					v = math.Max(-10, math.Min(30, v))
					lineSum += v
					lineN++
					ln++
				}
			}
			if mn == len(e.Harmonics) && fn == mn && ln == mn {
				frames++
				if mv > math.Max(fv, lv) {
					positive++
				}
			}
		}
		expected := float64(len(test) * len(e.Harmonics))
		q.Coverage = float64(modelN) / expected
		if modelN > 0 {
			v := modelSum / float64(modelN)
			q.Test = &v
		}
		if flatN > 0 {
			v := flatSum / float64(flatN)
			q.Flat = &v
		}
		if lineN > 0 {
			v := lineSum / float64(lineN)
			q.Linear = &v
		}
		if q.Coverage < .95 {
			reject("telemetry_or_band_coverage", "The frozen flight model cannot cover 95% of reserved measurements.")
		}
		if float64(flatN) < .95*expected || float64(lineN) < .95*expected {
			reject("baseline_coverage", "Steady/linear controls cannot cover the reserved comparison band.")
		}
		if ridgeN < max(5, int(.7*float64(len(test)))) {
			reject("ridge_coverage", "Too few reserved measured-ridge samples are supported.")
		} else {
			rmse := math.Sqrt(squared / float64(ridgeN))
			q.RMSE = &rmse
			limit := math.Max(1.5, hzSum/float64(ridgeN)*.012)
			if rmse > limit {
				reject("ridge_residual", fmt.Sprintf("Reserved ridge error %.2f Hz exceeds %.2f Hz.", rmse, limit))
			}
		}
		if q.Test != nil && q.Flat != nil && q.Linear != nil {
			margin := *q.Test - math.Max(0, math.Max(*q.Flat, *q.Linear))
			q.Margin = &margin
			weakest = math.Min(weakest, margin)
			if *q.Test < c.MinScore {
				reject("weak_validation_signal", fmt.Sprintf("Reserved signal %.2f dB is below %.2f dB.", *q.Test, c.MinScore))
			}
			if margin < 1 {
				reject("steady_or_linear_tone", "The flight model does not beat steady and linear-pitch controls by 1 dB on reserved audio.")
			}
		}
		if frames == 0 || float64(positive)/float64(max(1, frames)) < .65 {
			reject("inconsistent_validation", "Fewer than 65% of comparable reserved frames favour the flight model.")
		}
		q.Pass = len(q.Reasons) == 0
		all = all && q.Pass
		checks = append(checks, q)
	}
	if !geo.Finite(weakest) {
		return checks, nil, false
	}
	return checks, &weakest, all
}

// Parameters are fitted exclusively to training ridges; reserved samples never
// choose source frequency, clock, location, baseline pitch or baseline slope.
func trainingPitchBaselines(obs []scoreObservation) (flat, slope, ref float64, ok bool) {
	var n, x, y, xx, xy float64
	if len(obs) > 0 {
		ref = obs[0].time
	}
	for _, p := range obs {
		if p.hasRidge {
			u := p.time - ref
			n++
			x += u
			y += p.hz
			xx += u * u
			xy += u * p.hz
		}
	}
	den := n*xx - x*x
	if n < 5 || den <= 0 {
		return 0, 0, ref, false
	}
	slope = (n*xy - x*y) / den
	flat = y / n
	// Put the linear intercept at the mean time, where it equals the flat fit.
	ref += x / n
	return flat, slope, ref, true
}
