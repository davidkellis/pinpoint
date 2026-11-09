package search

import (
	"math"
	"sort"
	"time"

	"pinpoint/internal/acoustic"
	"pinpoint/internal/tracks"
)

// RecordingFlightWindow covers the entire STFT-supported recording, plus clock
// uncertainty, propagation time and interpolation margins. It deliberately
// does not depend on which sound events the detector found.
func RecordingFlightWindow(c Config, s *acoustic.Spectrum) (float64, float64) {
	t, _ := time.Parse(time.RFC3339Nano, c.RecordingStart)
	start := float64(t.Unix()) + float64(t.Nanosecond())/1e9
	duration := s.Time(len(s.Rows)-1) + float64(s.Window)/2/float64(s.SampleRate)
	return start - c.ClockUncertainty - 180, start + duration + c.ClockUncertainty + 60
}

// TracksForEvents reduces computation, not the recorded interval searched.
// Each event receives its own slice of the full recording's downloaded history.
// The boundary points retain every segment intersecting the possible emission
// interval, including the acoustic travel delay.
func TracksForEvents(c Config, ts []tracks.Track) []tracks.Track {
	t, err := time.Parse(time.RFC3339Nano, c.RecordingStart)
	if err != nil {
		return nil
	}
	start := float64(t.Unix()) + float64(t.Nanosecond())/1e9
	from, until := math.Inf(1), math.Inf(-1)
	for _, e := range c.Events {
		from = math.Min(from, start+e.Start-c.ClockUncertainty-e.MaxSlant/c.SoundSpeed-10)
		until = math.Max(until, start+e.End+c.ClockUncertainty+10)
	}
	for _, w := range c.Silence {
		from = math.Min(from, start+w.Start-c.ClockUncertainty-w.MaxSlant/c.SoundSpeed-10)
		until = math.Max(until, start+w.End+c.ClockUncertainty+10)
	}
	var out []tracks.Track
	for _, t := range ts {
		p := t.Points
		if len(p) < 2 || p[len(p)-1].Time < from || p[0].Time > until {
			continue
		}
		lo := max(0, sort.Search(len(p), func(i int) bool { return p[i].Time >= from })-1)
		hi := min(len(p), sort.Search(len(p), func(i int) bool { return p[i].Time > until })+1)
		if hi-lo >= 2 {
			out = append(out, tracks.Track{ID: t.ID, Points: p[lo:hi], TypeCode: t.TypeCode})
		}
	}
	return out
}
