package webapp

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"pinpoint/internal/acoustic"
	"pinpoint/internal/geo"
	"pinpoint/internal/search"
	"pinpoint/internal/tracks"
	"strings"
	"testing"
	"time"
)

func TestRecordingTimeZoneAndOptionalState(t *testing.T) {
	r := SearchRequest{LocalStart: "2026-11-03T18:50:11", TimeZone: "America/Chicago", State: "Texas"}
	if e := normalizeRequest(&r); e != nil {
		t.Fatal(e)
	}
	if r.RecordingStart != "2026-11-04T00:50:11Z" || !r.Bounds.Contains(31, -99) {
		t.Fatalf("wrong Texas time/area: %+v", r)
	}
	r = SearchRequest{RecordingStart: "2026-11-04T00:50:11Z"}
	if e := normalizeRequest(&r); e != nil || !r.Bounds.Contains(40, -120) || !r.Bounds.Contains(40, -74) {
		t.Fatal("missing state did not default to contiguous US")
	}
	for _, local := range []string{"2026-03-08T02:30:00", "2026-11-01T01:30:00"} {
		r = SearchRequest{LocalStart: local, TimeZone: "America/Chicago"}
		if normalizeRequest(&r) == nil {
			t.Fatal("accepted nonexistent/ambiguous DST time", local)
		}
	}
	r = SearchRequest{RecordingStart: "2026-11-04T00:50:11Z", State: "invalid", Bounds: geo.Bounds{South: 30, North: 31, West: -100, East: -99}}
	if normalizeRequest(&r) == nil {
		t.Fatal("accepted unknown state")
	}
}

func TestConcurrentEventsPreserveSerialReportsAndOrdering(t *testing.T) {
	s, _ := testServer(t)
	_, ts := recordingFixture(t)
	spectrum := &acoustic.Spectrum{Version: 1, SampleRate: 1024, Window: 1024, Hop: 1024, FirstBin: 40}
	for frame := 0; frame < 41; frame++ {
		row := make([]float32, 200)
		for k := range row {
			row[k] = 10
		}
		spectrum.Rows = append(spectrum.Rows, row)
	}
	c := search.DefaultConfig()
	c.RecordingStart = "2023-11-14T22:13:20Z"
	c.ClockUncertainty = 1
	c.Workers = 3
	c.Refine = 0
	c.Bounds = geo.Bounds{South: -.002, North: .002, West: -.002, East: .002}
	h := 0.0
	c.ObserverHeight = &h
	for k := 0; k < 4; k++ {
		c.Events = append(c.Events, acoustic.Event{Start: 5.5 + float64(k), End: 30.5, CPATime: 20, FMin: 130, FMax: 130, FStep: 1, Harmonics: []int{1}, MinSpeed: 15, MaxSpeed: 180, MaxSlant: 10000, Partial: true})
	}
	r := SearchRequest{Automatic: true, Events: c.Events}
	var baseline [][]byte
	for _, workers := range []int{1, 2, 4} {
		s.options.EventWorkers = workers
		j, err := s.newJob("parallel-test.wav")
		if err != nil {
			t.Fatal(err)
		}
		if err = s.findEvents(context.Background(), j, r, c, spectrum, ts); err != nil {
			t.Fatal(err)
		}
		info := j.snapshot()
		if info.Completed != 4 || info.Total != 4 || info.ResultURL != fmt.Sprintf("/api/jobs/%s/events/1/map.html", info.ID) {
			t.Fatalf("completion order changed status/result: %+v", info)
		}
		for k, m := range info.EventMaps {
			if m.Start != r.Events[k].Start || m.Status != "candidate_matches" {
				t.Fatalf("event result misplaced: %+v", m)
			}
			data, err := os.ReadFile(filepath.Join(j.dir, fmt.Sprintf("event-%02d", k+1), "results.json"))
			if err != nil {
				t.Fatal(err)
			}
			if workers == 1 {
				baseline = append(baseline, data)
			} else if !bytes.Equal(data, baseline[k]) {
				t.Fatalf("event %d scores changed with %d event workers", k+1, workers)
			}
		}
	}
}

func TestTerrainQueueWaitCanBeCancelled(t *testing.T) {
	s, _ := testServer(t)
	s.terrainWork <- struct{}{}
	defer func() { <-s.terrainWork }()
	c := search.DefaultConfig()
	c.Corridors = true
	c.RecordingStart = "2023-11-14T22:13:20Z"
	c.Bounds = geo.Bounds{South: 29, North: 30, West: -99, East: -98}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.prepareEventTerrain(ctx, c, nil, func(string, int, int) { cancel() }) }()
	select {
	case err := <-done:
		if err != context.Canceled {
			t.Fatalf("got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("terrain wait ignored cancellation")
	}
}

func TestUnavailableHistoryPreservesJobForRetry(t *testing.T) {
	s, _ := testServer(t)
	j, e := s.newJob("test.wav")
	if e != nil {
		t.Fatal(e)
	}
	s.start(j, "searching", func(context.Context) error { return &tracks.HistoryUnavailable{Message: "Not published yet"} })
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if j.snapshot().State == "waiting_data" {
			if j.snapshot().Error != "" {
				t.Fatal("pending history treated as fatal")
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("job not made retryable")
}

func TestAllEventOutcomesSurviveIndividualFailure(t *testing.T) {
	s, server := testServer(t)
	j, err := s.newJob("many-events.wav")
	if err != nil {
		t.Fatal(err)
	}
	spectrum := &acoustic.Spectrum{Version: 1, SampleRate: 1024, Window: 1024, Hop: 1024, FirstBin: 40}
	for frame := 0; frame < 200; frame++ {
		row := make([]float32, 200)
		for k := range row {
			row[k] = 10
		}
		spectrum.Rows = append(spectrum.Rows, row)
	}
	_, ts := recordingFixture(t)
	c := search.DefaultConfig()
	c.RecordingStart = "2023-11-14T22:13:20Z"
	c.ClockUncertainty = 0
	c.Refine = 0
	c.Bounds = geo.Bounds{South: -.00001, North: .00001, West: -.00001, East: .00001}
	h := 0.0
	c.ObserverHeight = &h
	for k := 0; k < 10; k++ {
		c.Events = append(c.Events, acoustic.Event{Start: 5.5, End: 30.5, CPATime: 20, FMin: 130, FMax: 130, FStep: 1, Harmonics: []int{1}, MinSpeed: 15, MaxSpeed: 180, MaxSlant: 10000, Partial: true})
	}
	c.Events[1].Start = 190.5
	c.Events[1].End = 199.5
	c.Events[1].CPATime = 195
	r := SearchRequest{Automatic: true, Events: c.Events}
	j.update(func(i *JobInfo) { i.Events = c.Events })
	// One result cannot be saved, but the remaining nine must still run.
	if err = os.WriteFile(filepath.Join(j.dir, "event-01"), []byte("storage failure"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = s.findEvents(context.Background(), j, r, c, spectrum, ts); err != nil {
		t.Fatal(err)
	}
	info := j.snapshot()
	if len(info.EventMaps) != 10 || info.ResultStatus != "partial" {
		t.Fatalf("lost failed events or partial status: %+v", info)
	}
	if info.EventMaps[0].Status != "error" || info.EventMaps[0].Error == "" || info.EventMaps[1].Status != "no_tracks" || info.EventMaps[9].Status != "candidate_matches" {
		t.Fatalf("incorrect outcomes: %+v", info.EventMaps)
	}
	response, err := http.Get(fmt.Sprintf("%s/api/jobs/%s/events/10/map.html", server.URL, info.ID))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	if response.StatusCode != 200 || !strings.Contains(string(body), "score_grid") {
		t.Fatal("later event map inaccessible")
	}
	// A cancellation must leave all candidates visible, including unsearched ones.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err = s.findEvents(ctx, j, r, c, spectrum, ts); err == nil {
		t.Fatal("ignored cancellation")
	}
	for _, m := range j.snapshot().EventMaps {
		if m.Status != "not_run" {
			t.Fatal("silently omitted cancelled event")
		}
	}
}

func TestExplicitJointModeRequiresStationaryReviewedEvents(t *testing.T) {
	s, _ := testServer(t)
	j, _ := s.newJob("joint.wav")
	spectrum := &acoustic.Spectrum{SampleRate: 1024, Window: 1024, Hop: 1024, FirstBin: 40}
	for i := 0; i < 100; i++ {
		spectrum.Rows = append(spectrum.Rows, make([]float32, 200))
	}
	event := acoustic.Event{Start: 5.5, End: 25.5, CPATime: 15, CPATolerance: 5, FMin: 100, FMax: 140, FStep: 1, Harmonics: []int{1}, MinSpeed: 15, MaxSpeed: 180, MaxSlant: 10000}
	h := 0.0
	r := defaultRequest()
	r.MatchMode = "joint"
	r.Grid = 200
	r.Source = "archive"
	r.RecordingStart = "2026-11-04T00:50:11Z"
	r.Bounds = geo.Bounds{South: 29, North: 30, West: -99, East: -98}
	r.Height = &h
	r.Events = []acoustic.Event{event, event}
	r.Moving = true
	if _, err := s.configuration(j, r, spectrum); err == nil {
		t.Fatal("accepted stationary joint model for moving observer")
	}
	r.Moving = false
	c, err := s.configuration(j, r, spectrum)
	if err != nil {
		t.Fatal(err)
	}
	if c.ExcludeJets {
		t.Fatal("jets excluded by default")
	}
	exclude := true
	r.ExcludeJets = &exclude
	c, err = s.configuration(j, r, spectrum)
	if err != nil || !c.ExcludeJets {
		t.Fatalf("explicit jet exclusion ignored: %v", err)
	}
	r.Events[0].Partial = true
	if _, err := s.configuration(j, r, spectrum); err == nil {
		t.Fatal("partial flyover accepted as measured CPA")
	}
	r.Events[0].CPATolerance = 0
	c, err = s.configuration(j, r, spectrum)
	if err != nil || !c.Staged || !c.Events[0].Partial || c.Events[0].CPATolerance != 0 {
		t.Fatalf("partial curve cannot share a clock without an invented CPA: %+v %v", c.Events, err)
	}
	r.Events[0].Partial = false
	r.Events[0].CPATolerance = 5
	if r.independent() {
		t.Fatal("joint mode silently used independent clocks")
	}
	r.MatchMode = "independent"
	r.Automatic = false
	if !r.independent() {
		t.Fatal("reviewed independent selection silently became joint")
	}
}

func TestNewUploadsDefaultToConsensusWithProgressiveRefinement(t *testing.T) {
	r := defaultRequest()
	if r.independent() || r.MatchMode != "consensus" || !r.Automatic || r.FinalGrid != 10 {
		t.Fatalf("new automatic uploads still use independent matching: %+v", r)
	}
	// Historical independent jobs retain their requested behavior.
	r.MatchMode = "independent"
	if !r.independent() {
		t.Fatal("changed historical independent run into a joint result")
	}
}

func TestWeakAutomaticWalkStopsBeforeDownloadingFlightHistory(t *testing.T) {
	s, server := testServer(t)
	j, err := s.newJob("weak walk.wav")
	if err != nil {
		t.Fatal(err)
	}
	spectrum := &acoustic.Spectrum{Version: 1, SampleRate: 1024, Window: 1024, Hop: 1024, FirstBin: 40, Rows: make([][]float32, 100)}
	for i := range spectrum.Rows {
		spectrum.Rows[i] = make([]float32, 200)
	}
	if err = spectrum.Save(filepath.Join(j.dir, "spectrogram.bin")); err != nil {
		t.Fatal(err)
	}
	report := acoustic.DetectionReport{Version: acoustic.DetectorVersion, Events: []acoustic.Event{{Start: 10, End: 60, Evidence: &acoustic.CurveEvidence{Level: "weak"}, Measurement: &acoustic.RidgeMeasurement{Usable: true}}}}
	if err = search.JSON(filepath.Join(j.dir, "detection.json"), report); err != nil {
		t.Fatal(err)
	}
	j.update(func(info *JobInfo) { info.State = "ready" })
	request := `{"automatic":true,"moving":true,"match_mode":"independent","state":"TX","recording_start":"2026-11-04T00:50:11Z"}`
	response, err := http.Post(server.URL+"/api/jobs/"+j.snapshot().ID+"/search", "application/json", strings.NewReader(request))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	if response.StatusCode != 400 || !strings.Contains(string(body), "No flyovers pass the acoustic evidence checks") {
		t.Fatalf("searched weak walking fragments: HTTP %d %s", response.StatusCode, body)
	}
	if j.snapshot().State != "ready" {
		t.Fatal("started an expensive search with no strong curves")
	}
}
