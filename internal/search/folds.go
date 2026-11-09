package search

import (
	"fmt"
	"math"
	"pinpoint/internal/acoustic"
	"sort"
)

// Alternating blocks have a full FFT-width guard between opposite folds. The
// proposal's observed extent sets the plan; flight identities/GPS never do.
type FoldPlan struct{ Start, End, Block, Guard float64 }

func eventFold(e acoustic.Event, s *acoustic.Spectrum) FoldPlan {
	window := math.Max(e.Resolution, float64(s.Window)/float64(s.SampleRate))
	return FoldPlan{e.Start, e.End, math.Max(2*window, math.Min(12, (e.End-e.Start)/4)), window / 2}
}
func (p FoldPlan) Contains(t float64, fold string) bool {
	if fold == "" {
		return true
	}
	if t < p.Start || t > p.End {
		return false
	}
	u := t - p.Start
	block := int(math.Floor(u / p.Block))
	phase := u - float64(block)*p.Block
	if phase < p.Guard || phase > p.Block-p.Guard {
		return false
	}
	return block%2 == 0 && fold == "train" || block%2 == 1 && fold == "validation"
}
func eventSpectrum(c Config, fallback *acoustic.Spectrum, i int) *acoustic.Spectrum {
	if len(c.eventSpectra) == len(c.Events) && c.eventSpectra[i] != nil {
		return c.eventSpectra[i]
	}
	return fallback
}
func loadEventSpectra(c *Config) error {
	if len(c.EventSpectra) > 0 && len(c.eventSpectra) == 0 {
		if len(c.EventSpectra) != len(c.Events) {
			return fmt.Errorf("event spectrum count does not match curves")
		}
		for _, path := range c.EventSpectra {
			s, err := acoustic.Load(path)
			if err != nil {
				return err
			}
			c.eventSpectra = append(c.eventSpectra, s)
		}
	}
	if len(c.eventSpectra) > 0 && len(c.eventSpectra) != len(c.Events) {
		return fmt.Errorf("event spectrum count does not match curves")
	}
	return nil
}
func prepareFoldObservations(s *acoustic.Spectrum, e acoustic.Event, fold string) []scoreObservation {
	if fold == "" {
		return prepareObservations(s, e)
	}
	plan := eventFold(e, s)
	step := math.Min(1, (e.End-e.Start)/48)
	stride := max(1, int(math.Round(step/s.Step())))
	first := max(0, int(math.Ceil((e.Start-s.Time(0))/s.Step())))
	last := min(len(s.Rows)-1, int(math.Floor((e.End-s.Time(0))/s.Step())))
	var out []scoreObservation
	for frame := first; frame <= last; frame += stride {
		t := s.Time(frame)
		if !plan.Contains(t, fold) {
			continue
		}
		hz, ok := foldRidgeHz(e, t, fold, plan)
		out = append(out, scoreObservation{frame, t, hz, ok})
	}
	return out
}

func foldRidgeHz(e acoustic.Event, t float64, fold string, plan FoldPlan) (float64, bool) {
	i := sort.Search(len(e.Ridge), func(i int) bool { return e.Ridge[i].Time >= t })
	if i < len(e.Ridge) && e.Ridge[i].Time == t {
		if plan.Contains(t, fold) {
			return e.Ridge[i].Hz, true
		}
		return 0, false
	}
	if i == 0 || i == len(e.Ridge) {
		return 0, false
	}
	a, b := e.Ridge[i-1], e.Ridge[i]
	if !plan.Contains(a.Time, fold) || !plan.Contains(b.Time, fold) || int(math.Floor((a.Time-plan.Start)/plan.Block)) != int(math.Floor((b.Time-plan.Start)/plan.Block)) {
		return 0, false
	}
	return e.RidgeHz(t)
}
