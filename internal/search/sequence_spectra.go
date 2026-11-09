package search

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"pinpoint/internal/acoustic"
	"sync"
)

func sequenceAudioDigest(ctx context.Context, path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("validation needs the original audio: %w", err)
	}
	defer f.Close()
	h := sha256.New()
	buf := make([]byte, 1<<20)
	for {
		if err = ctx.Err(); err != nil {
			return "", err
		}
		n, e := f.Read(buf)
		h.Write(buf[:n])
		if e == io.EOF {
			break
		}
		if e != nil {
			return "", e
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func sequenceSpectra(ctx context.Context, c Config, o SequenceOptions, key string, progress func(SequenceProgress)) ([]*acoustic.Spectrum, []string, error) {
	work, cancel := context.WithCancel(ctx)
	defer cancel()
	type result struct {
		i    int
		s    *acoustic.Spectrum
		path string
		err  error
	}
	jobs := make(chan int)
	results := make(chan result, o.Workers)
	var wg sync.WaitGroup
	for w := 0; w < o.Workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				e := c.Events[i]
				res := e.Resolution
				if res == 0 {
					res = 2
				}
				opt := acoustic.Defaults()
				opt.Channel = o.Channel
				if res <= 1 {
					opt = acoustic.ShortPassOptions()
					opt.MinHz = 40
					opt.Channel = o.Channel
				}
				opt.Window = int(math.Round(float64(opt.Rate) * res))
				opt.Hop = max(1, opt.Window/8)
				plan := eventFold(e, &acoustic.Spectrum{SampleRate: opt.Rate, Window: opt.Window})
				from, to := math.Max(0, e.Start-res), e.End+res
				payload, _ := json.Marshal(struct {
					Key      string
					Event    acoustic.Event
					Options  acoustic.Options
					From, To float64
				}{key, e, opt, from, to})
				sum := sha256.Sum256(payload)
				path := ""
				if o.CacheDir != "" {
					path = filepath.Join(o.CacheDir, "spectra", hex.EncodeToString(sum[:])+".bin")
				}
				var s *acoustic.Spectrum
				var err error
				if path != "" {
					s, err = acoustic.Load(path)
				}
				if s == nil || err != nil {
					s, err = acoustic.AnalyzeTrainingInterval(work, o.AudioPath, opt, from, to, func(t float64) bool { return plan.Contains(t, "train") })
					if err == nil && path != "" {
						err = os.MkdirAll(filepath.Dir(path), 0700)
						if err == nil {
							f, e := os.CreateTemp(filepath.Dir(path), ".spectrum-*")
							if e != nil {
								err = e
							} else {
								f.Close()
								err = s.Save(f.Name())
								if err == nil {
									err = os.Rename(f.Name(), path)
								}
								os.Remove(f.Name())
							}
						}
					}
				}
				results <- result{i, s, path, err}
			}
		}()
	}
	go func() {
		defer close(jobs)
		for i := range c.Events {
			select {
			case jobs <- i:
			case <-work.Done():
				return
			}
		}
	}()
	go func() { wg.Wait(); close(results) }()
	bank := make([]*acoustic.Spectrum, len(c.Events))
	paths := make([]string, len(c.Events))
	var failure error
	done := 0
	for r := range results {
		done++
		if r.err != nil && failure == nil {
			failure = r.err
			cancel()
		}
		bank[r.i], paths[r.i] = r.s, r.path
		if progress != nil {
			progress(SequenceProgress{Stage: "preparing disjoint training and validation spectra", Done: done, Total: len(c.Events)})
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if failure != nil {
		return nil, nil, failure
	}
	if o.CacheDir == "" {
		paths = nil
	}
	return bank, paths, nil
}

func trainSequenceConfig(c Config, bank []*acoustic.Spectrum, paths []string) (Config, error) {
	c.Events = append([]acoustic.Event(nil), c.Events...)
	c.eventSpectra, c.EventSpectra = bank, paths
	c.ObservationFold = "train"
	c.ScoreObjective = "weakest"
	c.DistinctAircraft = true
	c.CurveEnvelope = true
	for i, e := range c.Events {
		s := bank[i]
		obs := prepareFoldObservations(s, e, "train")
		test := prepareFoldObservations(s, e, "validation")
		if len(obs) < 5 || len(test) < 5 {
			return c, fmt.Errorf("curve %d has insufficient disjoint samples (%d training, %d validation)", i+1, len(obs), len(test))
		}
		lo, hi := math.Inf(1), 0.
		for _, p := range obs {
			if p.hasRidge {
				lo = math.Min(lo, p.hz)
				hi = math.Max(hi, p.hz)
			}
		}
		if !geoFinitePositive(lo) || hi <= 0 {
			return c, fmt.Errorf("curve %d has no training ridge", i+1)
		}
		e.Partial = true
		e.CPATolerance = 0
		e.CPATime = (e.Start + e.End) / 2
		e.FMin = math.Max(1, lo*(1-e.MaxSpeed/c.SoundSpeed))
		e.FMax = hi * (1 + e.MaxSpeed/c.SoundSpeed)
		// Full-proposal shape checks are not reused as fitted parameter bounds.
		e.Measurement = &acoustic.RidgeMeasurement{ShapePass: true, Reason: "Flight parameters are fitted on guarded training intervals. Full-proposal ideal-fit parameter bounds are not used."}
		c.Events[i] = e
	}
	return c, nil
}
func geoFinitePositive(x float64) bool { return x > 0 && !math.IsInf(x, 0) && !math.IsNaN(x) }

func subsetSequenceConfig(c Config, indices []int) Config {
	events := make([]acoustic.Event, len(indices))
	bank := make([]*acoustic.Spectrum, len(indices))
	var paths []string
	for j, i := range indices {
		events[j] = c.Events[i]
		bank[j] = c.eventSpectra[i]
		if len(c.EventSpectra) > 0 {
			paths = append(paths, c.EventSpectra[i])
		}
	}
	c.Events = events
	c.eventSpectra = bank
	c.EventSpectra = paths
	c.eventTrackIDs = nil
	c.clockOffsets = nil
	return c
}
