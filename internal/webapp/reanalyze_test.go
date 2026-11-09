package webapp

import (
	"net/http"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"pinpoint/internal/acoustic"
	"pinpoint/internal/geo"
)

func TestReanalyzeUsesCachedSpectraAndPreservesSearchSettings(t *testing.T) {
	s, server := testServer(t)
	j, err := s.newJob("cached recording.mp3")
	if err != nil {
		t.Fatal(err)
	}
	// No audio file: this must succeed using the cached spectra, not decoding.
	for _, cfg := range []struct {
		name        string
		window, hop int
	}{
		{"spectrogram.bin", 16384, 2048}, {"detection-spectrum.bin", 8192, 1024},
	} {
		spectrum := &acoustic.Spectrum{Version: 1, SampleRate: 4096, Window: cfg.window, Hop: cfg.hop, FirstBin: cfg.window * 40 / 4096}
		for i := 0; i < 140; i++ {
			spectrum.Rows = append(spectrum.Rows, make([]float32, cfg.window*200/4096+1))
		}
		if err = spectrum.Save(filepath.Join(j.dir, cfg.name)); err != nil {
			t.Fatal(err)
		}
	}
	params := &SearchRequest{State: "TX", LocalStart: "2026-11-03T18:50:11", TimeZone: "America/Chicago", Moving: true, Bounds: geo.Bounds{South: 29, North: 30, West: -99, East: -98}}
	j.update(func(i *JobInfo) {
		i.State = "complete"
		i.Params = params
		i.HasTracks = true
		i.ResultURL = "stale-map.html"
		i.EventMaps = []EventMap{{}}
		i.Detection = &acoustic.DetectionReport{Version: "old"}
		i.Events = []acoustic.Event{{Start: 1, End: 20}}
	})
	url := server.URL + "/api/jobs/" + j.snapshot().ID
	// Hold the worker slot so the queued state and duplicate-request rejection
	// are deterministic without sleeps.
	func() {
		s.work <- struct{}{}
		defer func() { <-s.work }()
		response, err := http.Post(url+"/reanalyze", "application/json", nil)
		if err != nil {
			t.Fatal(err)
		}
		decodeResponse(t, response, http.StatusAccepted, nil)
		response, err = http.Post(url+"/reanalyze", "application/json", nil)
		if err != nil {
			t.Fatal(err)
		}
		decodeResponse(t, response, http.StatusConflict, nil)
	}()
	info := waitJob(t, url, "ready")
	if info.Detection == nil || info.Detection.Version != acoustic.DetectorVersion || len(info.Events) != 0 || info.ResultURL != "" || len(info.EventMaps) != 0 {
		t.Fatalf("stale analysis/results survived: %+v", info)
	}
	if !info.HasTracks || !reflect.DeepEqual(info.Params, params) {
		t.Fatal("search settings or flight data were lost")
	}
	if !strings.Contains(info.Message, "No clear flyover") {
		t.Fatal(info.Message)
	}
}
