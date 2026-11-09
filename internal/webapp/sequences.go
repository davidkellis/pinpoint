package webapp

import (
	"context"
	"fmt"
	"math"
	"path/filepath"
	"pinpoint/internal/acoustic"
	"pinpoint/internal/search"
	"pinpoint/internal/tracks"
	"time"
)

func (s *Server) findSequences(ctx context.Context, j *job, c search.Config, spectrum *acoustic.Spectrum, ts []tracks.Track) error {
	o := search.DefaultSequenceOptions(c)
	o.CacheDir = filepath.Join(j.dir, "sequence-cache")
	o.AudioPath = filepath.Join(j.dir, "audio")
	terrain := c
	for _, shift := range o.ControlShifts {
		terrain.ClockUncertainty = math.Max(terrain.ClockUncertainty, c.ClockUncertainty+math.Abs(shift))
	}
	if err := s.prepareTerrain(ctx, j, terrain, ts); err != nil {
		return err
	}
	j.update(func(i *JobInfo) { i.EventMaps = nil; i.ResultURL = ""; i.ResultStatus = "" })
	last := time.Time{}
	r, err := search.RunAutomaticSequences(ctx, c, spectrum, ts, o, func(p search.SequenceProgress) {
		if p.Stage != "starting" && p.Stage != "finished" && time.Since(last) < 500*time.Millisecond {
			return
		}
		last = time.Now()
		j.update(func(i *JobInfo) {
			i.Phase = "scoring"
			if p.Trials == 0 {
				i.Message = fmt.Sprintf("Preparing training and reserved sound · %d/%d curves", p.Done, p.Total)
			} else {
				i.Message = fmt.Sprintf("Trial %d/%d (%+.0f s), %d-flyover group %d/%d: %s · %d/%d", p.Trial, p.Trials, p.Shift, p.Size, p.Group, p.Groups, p.Stage, p.Done, p.Total)
			}
			i.Completed = int64(p.Done)
			i.Total = int64(p.Total)
		})
	}, func(g search.SequenceGroup, group search.Config, r search.Report) error {
		if g.Trial != 0 || g.Best == nil {
			return nil
		}
		dir := filepath.Join(j.dir, fmt.Sprintf("event-%02d", g.ID+1))
		r.Note += " Training-only sound-group hypothesis. Reserved-audio checks and complete wrong-time controls are reported in the group overview; this map alone does not establish a location."
		if err := search.WriteReport(dir, r, group, spectrum, ts); err != nil {
			return err
		}
		if err := search.JSON(filepath.Join(dir, "search.json"), group); err != nil {
			return err
		}
		if err := search.JSON(filepath.Join(dir, "validation.json"), g); err != nil {
			return err
		}
		j.update(func(i *JobInfo) {
			i.EventMaps = append(i.EventMaps, EventMap{Label: "Curves " + search.CurveGroupLabel(g.Indices), URL: fmt.Sprintf("/api/jobs/%s/events/%d/map.html", i.ID, g.ID+1), Status: g.Status, Start: group.Events[0].Start, End: group.Events[len(group.Events)-1].End, Matches: len(r.Candidates), Tracks: len(ts), Message: "Training fit; reserved audio: " + g.Status})
		})
		return nil
	})
	writeErr := search.WriteSequenceReport(j.dir, r, func(id int) string { return fmt.Sprintf("/api/jobs/%s/events/%d/map.html", j.snapshot().ID, id+1) })
	if err != nil {
		return err
	}
	if writeErr != nil {
		return writeErr
	}
	if err = search.JSON(filepath.Join(j.dir, "search.json"), c); err != nil {
		return err
	}
	j.update(func(i *JobInfo) {
		i.State = "complete"
		i.Phase = "complete"
		i.ResultStatus = r.Status
		i.ResultURL = "/api/jobs/" + i.ID + "/files/automatic.html"
		i.Message = fmt.Sprintf("Sequence search: %s. Review the selected group, every aircraft's reserved-audio checks, rejection reasons and wrong-time controls. No independently verified GPS is claimed.", r.Status)
	})
	return nil
}
