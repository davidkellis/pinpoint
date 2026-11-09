package webapp

import (
	"context"
	"fmt"
	"path/filepath"
	"pinpoint/internal/acoustic"
	"pinpoint/internal/search"
	"pinpoint/internal/tracks"
	"time"
)

func (s *Server) findGroups(ctx context.Context, j *job, c search.Config, spectrum *acoustic.Spectrum, ts []tracks.Track) error {
	o := search.DefaultAutomaticOptions(c)
	o.CacheDir = filepath.Join(j.dir, "group-cache")
	terrainConfig := c
	terrainConfig.ClockUncertainty += o.ControlShifts[len(o.ControlShifts)-1]
	if err := s.prepareTerrain(ctx, j, terrainConfig, ts); err != nil {
		return err
	}
	j.update(func(i *JobInfo) { i.EventMaps = nil; i.ResultURL = ""; i.ResultStatus = "" })
	last := time.Time{}
	r, err := search.RunAutomatic(ctx, c, spectrum, ts, o, func(p search.GroupProgress) {
		if p.Stage != "starting" && p.Stage != "finished" && time.Since(last) < 500*time.Millisecond {
			return
		}
		last = time.Now()
		j.update(func(i *JobInfo) {
			i.Phase = "scoring"
			i.Message = fmt.Sprintf("Trial %d/%d (%+.0f s), sound pair %d/%d: %s · %d/%d", p.Trial, p.Trials, p.Shift, p.Pair, p.Pairs, p.Stage, p.Done, p.Total)
			i.Completed = int64(p.Done)
			i.Total = int64(p.Total)
		})
	}, func(g search.GroupResult, group search.Config, r search.Report) error {
		if g.Trial != 0 || g.Best == nil {
			return nil
		}
		dir := filepath.Join(j.dir, fmt.Sprintf("event-%02d", g.Pair+1))
		r.Note += " Automatic sound-pair hypothesis. Its whole-search timing controls and competing sound groups are reported separately; this map alone does not establish a recording location."
		if err := search.WriteReport(dir, r, group, spectrum, ts); err != nil {
			return err
		}
		if err := search.JSON(filepath.Join(dir, "search.json"), group); err != nil {
			return err
		}
		j.update(func(i *JobInfo) {
			i.EventMaps = append(i.EventMaps, EventMap{Label: fmt.Sprintf("Curves %d + %d · provisional", g.Events[0]+1, g.Events[1]+1), URL: fmt.Sprintf("/api/jobs/%s/events/%d/map.html", i.ID, g.Pair+1), Status: "candidate_matches", Start: group.Events[0].Start, End: group.Events[1].End, Matches: len(r.Candidates), Tracks: len(ts)})
		})
		return nil
	})
	writeErr := search.WriteAutomaticReport(j.dir, r, func(pair int) string { return fmt.Sprintf("/api/jobs/%s/events/%d/map.html", j.snapshot().ID, pair+1) })
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
		i.Message = fmt.Sprintf("Automatic group search: %s. Review the sound pairs, competing fits and wrong-time controls. No independently verified position is claimed.", r.Status)
	})
	return nil
}
