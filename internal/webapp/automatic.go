package webapp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
	_ "time/tzdata"

	"pinpoint/internal/acoustic"
	"pinpoint/internal/geo"
	"pinpoint/internal/search"
	"pinpoint/internal/tracks"
)

type EventMap struct {
	Label     string  `json:"label"`
	URL       string  `json:"url"`
	Status    string  `json:"status"`
	Start     float64 `json:"start_s"`
	End       float64 `json:"end_s"`
	Error     string  `json:"error,omitempty"`
	Tracks    int     `json:"tracks"`
	Matches   int     `json:"matching_locations"`
	Phase     string  `json:"phase,omitempty"`
	Message   string  `json:"message,omitempty"`
	Completed int     `json:"completed,omitempty"`
	Total     int     `json:"total,omitempty"`
}

func normalizeRequest(r *SearchRequest) error {
	if r.LocalStart != "" {
		zone, err := time.LoadLocation(r.TimeZone)
		if err != nil || r.TimeZone == "" {
			return fmt.Errorf("Choose a valid recording time zone")
		}
		layout := "2006-01-02T15:04:05"
		if len(r.LocalStart) == 16 {
			layout = "2006-01-02T15:04"
		}
		t, err := time.ParseInLocation(layout, r.LocalStart, zone)
		if err != nil || t.Format(layout) != r.LocalStart {
			return fmt.Errorf("The local recording time is invalid in this time zone")
		}
		if t.Add(time.Hour).Format(layout) == r.LocalStart || t.Add(-time.Hour).Format(layout) == r.LocalStart {
			return fmt.Errorf("This time occurs twice at the daylight-saving transition. Use UTC with the correct hour instead")
		}
		r.RecordingStart = t.UTC().Format(time.RFC3339)
	}
	if r.RecordingStart == "" {
		return fmt.Errorf("A recording date and approximate start time are needed to select flight history. The audio alone does not contain a reliable date")
	}
	b, ok := geo.StateBounds(r.State)
	if !ok {
		return fmt.Errorf("Choose a state in the contiguous US")
	}
	if r.Bounds == (geo.Bounds{}) {
		r.Bounds = b
	}
	return nil
}

func (s *Server) prepareTerrain(ctx context.Context, j *job, c search.Config, ts []tracks.Track) error {
	return s.prepareEventTerrain(ctx, c, ts, func(message string, done, total int) {
		j.update(func(i *JobInfo) {
			i.Phase, i.Message = "terrain", message
			i.Completed, i.Total = int64(done), int64(total)
		})
	})
}

func (s *Server) prepareEventTerrain(ctx context.Context, c search.Config, ts []tracks.Track, progress func(string, int, int)) error {
	if c.ObserverHeight != nil {
		return nil
	}
	if c.ExcludeJets {
		ts, _ = tracks.ExcludeKnownJets(ts)
	}
	progress("Preparing terrain elevations for candidate flight corridors", 0, 0)
	ordered, err := search.TerrainTiles(ctx, c, ts)
	if err != nil {
		return err
	}
	// Concurrent events may need the same tiles. Coordinate cache writes and
	// retain one four-download budget, while other events continue scoring.
	select {
	case s.terrainWork <- struct{}{}:
		defer func() { <-s.terrainWork }()
	case <-ctx.Done():
		return ctx.Err()
	}
	if _, err = geo.DownloadGeoid(ctx, filepath.Join(s.options.DataDir, "cache")); err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	tasks := make(chan string, len(ordered))
	results := make(chan error, len(ordered))
	for _, name := range ordered {
		tasks <- name
	}
	close(tasks)
	var wg sync.WaitGroup
	for worker := 0; worker < 4; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for name := range tasks {
				if e := ctx.Err(); e != nil {
					results <- e
					continue
				}
				results <- geo.DownloadTerrain(ctx, c.TerrainDir, name)
			}
		}()
	}
	go func() { wg.Wait(); close(results) }()
	done := 0
	var failure error
	for e := range results {
		done++
		if e != nil && failure == nil {
			failure = e
			cancel()
		}
		progress(fmt.Sprintf("Preparing terrain: %d of %d tiles", done, len(ordered)), done, len(ordered))
	}
	if failure != nil {
		return failure
	}
	return nil
}

func (s *Server) findEvents(ctx context.Context, j *job, r SearchRequest, c search.Config, spectrum *acoustic.Spectrum, ts []tracks.Track) error {
	j.initEvents(r.Events)
	defer j.finishPendingEvents()
	pool := search.NewWorkerPool(c.Workers)
	tasks := make(chan int)
	var wg sync.WaitGroup
	// Limit concurrent grids/terrain/report allocations as well as scoring CPUs.
	for worker := 0; worker < min(s.options.EventWorkers, c.Workers, len(r.Events)); worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for k := range tasks {
				if ctx.Err() == nil {
					s.findIndependentEvent(ctx, j, r, c, spectrum, ts, pool, k)
				}
			}
		}()
	}
schedule:
	for k := range r.Events {
		select {
		case tasks <- k:
		case <-ctx.Done():
			break schedule
		}
	}
	close(tasks)
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := search.JSON(filepath.Join(j.dir, "search-request.json"), r); err != nil {
		return err
	}
	j.update(func(i *JobInfo) {
		completed, matched, failed := eventCounts(i.EventMaps)
		i.State, i.Phase = "complete", "complete"
		i.ResultStatus = "no_match"
		if matched > 0 {
			i.ResultStatus = "candidate_matches"
		}
		if failed > 0 {
			i.ResultStatus = "partial"
		}
		i.Completed, i.Total = int64(completed+failed), int64(len(r.Events))
		i.Message = fmt.Sprintf("%d/%d sound events searched: %d with candidate matches, %d without a match, %d failed. Matches are unverified positions.", completed, len(r.Events), matched, completed-matched, failed)
		if r.Moving {
			i.Message += " Walking motion is approximated within each event; no route is inferred."
		}
	})
	return nil
}

func (s *Server) findIndependentEvent(ctx context.Context, j *job, r SearchRequest, c search.Config, spectrum *acoustic.Spectrum, ts []tracks.Track, pool *search.WorkerPool, k int) {
	one := c
	one.Events = []acoustic.Event{r.Events[k]}
	eventTracks := search.TracksForEvents(one, ts)
	j.setEvent(k, func(m *EventMap) { m.Status = "searching"; m.Tracks = len(eventTracks) })
	last := time.Time{}
	lastPhase := ""
	progress := func(phase, message string, done, total int) {
		if phase == lastPhase && done != 0 && done != total && time.Since(last) < 500*time.Millisecond {
			return
		}
		last, lastPhase = time.Now(), phase
		j.setEvent(k, func(m *EventMap) {
			m.Phase, m.Message, m.Completed, m.Total = phase, message, done, total
		})
	}
	fail := func(err error) {
		if ctx.Err() == nil {
			j.setEvent(k, func(m *EventMap) { m.Status = "error"; m.Error = err.Error() })
		}
	}
	if err := s.prepareEventTerrain(ctx, one, eventTracks, func(message string, done, total int) { progress("terrain", message, done, total) }); err != nil {
		fail(err)
		return
	}
	report, err := search.RunWithPool(ctx, one, spectrum, eventTracks, pool, func(stage string, done, total int) {
		progress(stage, fmt.Sprintf("%s: %d of %d locations", stage, done, total), done, total)
	})
	if err != nil {
		fail(err)
		return
	}
	report.Note += " Each selected acoustic candidate is searched independently. A high-scoring single-event match is unverified; unrelated moving sound sources may fit."
	if r.Moving {
		report.Note += " Walking mode: the stationary approximation applies only to this short event. Receiver motion is not fitted and can bias location. These maps do not reconstruct a route."
	}
	if n := j.snapshot().MissingTraces; n > 0 {
		report.Note += fmt.Sprintf(" %d aircraft traces were unavailable; flight coverage is incomplete.", n)
	}
	progress("mapping", "Writing the event map and curve comparisons", 0, 0)
	dir := filepath.Join(j.dir, fmt.Sprintf("event-%02d", k+1))
	if err = search.WriteReport(dir, report, one, spectrum, eventTracks); err == nil {
		err = search.JSON(filepath.Join(dir, "search.json"), one)
	}
	if err != nil {
		fail(err)
		return
	}
	id := j.snapshot().ID
	j.setEvent(k, func(m *EventMap) {
		m.URL = fmt.Sprintf("/api/jobs/%s/events/%d/map.html", id, k+1)
		m.Status, m.Phase, m.Message = report.Status, "complete", ""
		m.Completed, m.Total = 0, 0
		for _, p := range report.ScoreGrid {
			if p.MeetsThreshold {
				m.Matches++
			}
		}
		if len(eventTracks) == 0 {
			m.Status = "no_tracks"
		}
	})
}

func eventCounts(events []EventMap) (completed, matched, failed int) {
	for _, e := range events {
		switch e.Status {
		case "candidate_matches":
			completed++
			matched++
		case "no_match", "no_tracks":
			completed++
		case "error", "not_run":
			failed++
		}
	}
	return
}

// Completion order must not determine the default result or event ordering.
func updateEventSummary(i *JobInfo) {
	completed, _, failed := eventCounts(i.EventMaps)
	active := []int{}
	first, matched := "", ""
	for k, e := range i.EventMaps {
		if e.Status == "searching" {
			active = append(active, k+1)
		}
		if first == "" && e.URL != "" {
			first = e.URL
		}
		if matched == "" && e.Status == "candidate_matches" {
			matched = e.URL
		}
	}
	i.ResultURL = first
	if matched != "" {
		i.ResultURL = matched
	}
	i.Phase = "events"
	i.Completed, i.Total = int64(completed+failed), int64(len(i.EventMaps))
	i.Message = fmt.Sprintf("%d/%d events finished · %d active %v", completed+failed, len(i.EventMaps), len(active), active)
	if failed > 0 {
		i.Message += fmt.Sprintf(" · %d failed", failed)
	}
}

func (j *job) initEvents(events []acoustic.Event) {
	j.update(func(i *JobInfo) {
		i.ResultURL, i.ResultStatus = "", ""
		i.EventMaps = make([]EventMap, len(events))
		for k, e := range events {
			i.EventMaps[k] = EventMap{Label: fmt.Sprintf("Event %d · %.0f–%.0f s", k+1, e.Start, e.End), Start: e.Start, End: e.End, Status: "pending"}
		}
	})
}

// Replace the slice on update so a concurrent status response can safely retain
// a snapshot while JSON is being encoded outside the job lock.
func (j *job) setEvent(k int, change func(*EventMap)) {
	j.update(func(i *JobInfo) {
		i.EventMaps = append([]EventMap(nil), i.EventMaps...)
		change(&i.EventMaps[k])
		updateEventSummary(i)
	})
}
func (j *job) finishPendingEvents() {
	j.update(func(i *JobInfo) {
		i.EventMaps = append([]EventMap(nil), i.EventMaps...)
		for k := range i.EventMaps {
			if i.EventMaps[k].Status == "pending" || i.EventMaps[k].Status == "searching" {
				i.EventMaps[k].Status = "not_run"
				i.EventMaps[k].Error = "Run interrupted before this event completed. Retry to search it."
			}
		}
	})
}

func (s *Server) hasCachedArchives(a, b float64) bool {
	for t := time.Unix(int64(a), 0).UTC().Truncate(24 * time.Hour); !t.After(time.Unix(int64(b), 0)); t = t.AddDate(0, 0, 1) {
		if _, e := os.Stat(filepath.Join(s.options.DataDir, "cache", t.Format("2006-01-02"), "prod-0", "manifest.json")); e != nil {
			return false
		}
	}
	return true
}
