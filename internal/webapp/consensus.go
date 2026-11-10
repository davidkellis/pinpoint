package webapp

import (
	"context"
	"fmt"
	"math"
	"path/filepath"
	"time"

	"pinpoint/internal/acoustic"
	"pinpoint/internal/geo"
	"pinpoint/internal/search"
	"pinpoint/internal/tracks"
)

// consensusCurves picks the curves offered to a consensus search: every strong,
// plausible-pitch curve when automatic, otherwise the reviewed selection.
func consensusCurves(r SearchRequest, all []acoustic.Event) ([]acoustic.Event, []acoustic.LocatableCurve) {
	if r.Automatic {
		return acoustic.LocatableCurves(all, acoustic.DefaultLocatableMaxHz, 16)
	}
	var events []acoustic.Event
	var info []acoustic.LocatableCurve
	for i, e := range r.Events {
		if len(e.Ridge) < 8 {
			continue
		}
		strong := e
		if strong.Evidence == nil || strong.Evidence.Level != "strong" {
			copied := acoustic.CurveEvidence{Level: "strong", Reasons: []string{"reviewed by the user"}}
			strong.Evidence = &copied
		}
		got, picked := acoustic.LocatableCurves([]acoustic.Event{strong}, math.Inf(1), 0)
		if len(got) == 1 {
			picked[0].Index = i + 1
			events = append(events, e)
			info = append(info, picked[0])
		}
	}
	return events, info
}

// consensusOptions maps a website request onto a consensus search: a 1 km
// coarse grid for large areas, at most about 61 coarse clock steps, then finer
// clocks and grids around the best separated seeds.
func consensusOptions(c search.Config, workers int) search.ConsensusOptions {
	o := search.DefaultConsensusOptions()
	o.Bounds, o.RegionMask, o.RecordingStart = c.Bounds, c.RegionMask, c.RecordingStart
	span := geo.ECEF(c.Bounds.South, c.Bounds.West, 0).Sub(geo.ECEF(c.Bounds.North, c.Bounds.East, 0)).Norm()
	if span < 60000 {
		o.Grid, o.Refine = 250, 50
	}
	if c.FinalGrid > 0 {
		o.Final = math.Min(o.Refine, math.Max(5, c.FinalGrid))
	}
	o.ClockWindow = c.ClockUncertainty
	o.ClockStep = math.Max(2, math.Ceil(2*o.ClockWindow/60))
	o.FineClockStep = math.Min(o.ClockStep, 1)
	o.Workers = max(1, workers)
	return o
}

func (s *Server) findConsensus(ctx context.Context, j *job, r SearchRequest, c search.Config, ts []tracks.Track) error {
	events, curves := consensusCurves(r, j.snapshot().Events)
	o := consensusOptions(c, s.options.Workers)
	o.ExcludeKnownJets = c.ExcludeJets
	if len(events) < o.MinCurves {
		return fmt.Errorf("Consensus search needs at least %d strong curves below %.0f Hz; this recording has %d. Review the detected curves, or use another matching method. No location is substituted.", o.MinCurves, acoustic.DefaultLocatableMaxHz, len(events))
	}
	terrain := c
	terrain.Events = events
	if err := s.prepareTerrain(ctx, j, terrain, ts); err != nil {
		return err
	}
	if c.ObserverHeight != nil {
		h := *c.ObserverHeight
		o.Height = func(float64, float64) (float64, error) { return h, nil }
	} else {
		heights, err := geo.NewHeightSampler(c.TerrainDir, c.GeoidGrid, c.MicHeight, true)
		if err != nil {
			return err
		}
		o.Height = heights.Func(ctx)
	}
	j.phase("loudness", "Measuring the recording's loudness for the close-pass check")
	if l, err := acoustic.LoudnessEnvelope(ctx, filepath.Join(j.dir, "audio"), 0, 50, 2000, 1); err == nil {
		o.Loudness = &l
	}
	// Track reconstruction and reported-motion interpolation, as in the CLI.
	model := make([]tracks.Track, len(ts))
	for i, t := range ts {
		estimate, _, err := t.Reconstruct(tracks.DefaultReconstructionOptions())
		if err != nil {
			estimate = t
		}
		if smoothed, _, err := estimate.Smooth(tracks.SmoothingOptions{Step: .25, MaxGap: 30, MaxAcceleration: 20, UseKinematics: true}); err == nil {
			estimate = smoothed
		}
		model[i] = estimate
	}
	j.update(func(i *JobInfo) { i.EventMaps = nil; i.ResultURL = ""; i.ResultStatus = "" })
	stages := map[string]string{"grid": "coarse", "refine": "refined", "final": "final"}
	last := time.Time{}
	o.Progress = func(stage string, done, total int, best float64) {
		if done != total && time.Since(last) < 500*time.Millisecond {
			return
		}
		last = time.Now()
		j.update(func(i *JobInfo) {
			i.Phase = "scoring"
			i.Message = fmt.Sprintf("Consensus of %d curves: %s grid, %d of %d locations", len(events), stages[stage], done, total)
			i.Completed, i.Total = int64(done), int64(total)
		})
	}
	j.phase("scoring", fmt.Sprintf("Looking for locations where at least %d of %d curves agree under one clock", o.MinCurves, len(events)))
	res, err := search.Consensus(ctx, events, model, o)
	if err != nil {
		return err
	}
	for i := range res.Regions {
		for k := range res.Regions[i].Matches {
			res.Regions[i].Matches[k].Event = curves[res.Regions[i].Matches[k].Event-1].Index
		}
	}
	extra := map[string]any{"tracks": c.Tracks, "loudness_checked": o.Loudness != nil, "receiver_truth": "not supplied"}
	if err = search.WriteConsensusResult(j.dir, res, curves, extra); err != nil {
		return err
	}
	messages := map[string]string{
		"resolved":     "One area explains the most curves best and is not contradicted by loudness. This is a candidate, not a verified position.",
		"ambiguous":    fmt.Sprintf("Several separated areas fit almost equally well (spread %.1f km). The map lists them all; none is verified.", res.AgreementRadius/1000),
		"contradicted": "The best-fitting area is contradicted by loudness: aircraft predicted to pass close by are not heard. Treat every candidate as doubtful.",
		"no_match":     "No location explained enough curves with distinct aircraft under one clock.",
	}
	j.update(func(i *JobInfo) {
		i.State, i.Phase = "complete", "complete"
		i.ResultStatus = res.Status
		i.ResultURL = "/api/jobs/" + i.ID + "/files/consensus.html"
		i.Message = "Consensus search: " + messages[res.Status]
	})
	return nil
}
