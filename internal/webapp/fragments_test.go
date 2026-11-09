package webapp

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"pinpoint/internal/acoustic"
	"pinpoint/internal/search"
)

func TestLargeRecordingStatusPersistenceAndPagingPreserveEveryCurve(t *testing.T) {
	s, server := testServer(t)
	j, err := s.newJob("long-recording.mp3")
	if err != nil {
		t.Fatal(err)
	}
	events := make([]acoustic.Event, 1105)
	for i := range events {
		level := "weak"
		if i == 10 || i == 500 {
			level = "strong"
		}
		if i == 1000 {
			level = "review"
		}
		e := acoustic.Event{Start: float64(i), End: float64(i + 10), Evidence: &acoustic.CurveEvidence{Level: level}}
		for k := 0; k < 20; k++ {
			e.Ridge = append(e.Ridge, acoustic.RidgeSample{Time: float64(i) + float64(k)*.5, Hz: 130 - float64(k)*.1})
		}
		events[i] = e
	}
	d := acoustic.DetectionReport{Version: acoustic.HybridVersion, ScannedEnd: 1120}
	d.SetEvents(events)
	if err = search.JSON(filepath.Join(j.dir, "events.json"), events); err != nil {
		t.Fatal(err)
	}
	j.update(func(i *JobInfo) {
		i.Events = events
		i.Detection = &d
		i.State = "ready"
		i.Params = &SearchRequest{Automatic: true, Events: events, State: "TX"}
	})
	for k := 0; k < 3; k++ {
		j.update(func(i *JobInfo) { i.Completed++ })
	}
	b, err := os.ReadFile(filepath.Join(j.dir, "job.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(b) > 64<<10 || bytes.Contains(b, []byte(`"ridge"`)) {
		t.Fatal("progress persisted full acoustic traces")
	}
	var info JobInfo
	res, err := http.Get(server.URL + "/api/jobs/" + j.snapshot().ID)
	if err != nil {
		t.Fatal(err)
	}
	decodeResponse(t, res, 200, &info)
	if info.FragmentCount != len(events) || !info.EventsFiltered || len(info.Events) != 3 || len(info.EventIndices) != 3 || info.Detection == nil || len(info.Detection.Events) != 0 {
		t.Fatal("status duplicated or lost acoustic data")
	}
	if info.Params == nil || !info.Params.Automatic || info.Params.State != "TX" || len(info.Params.Events) != 0 {
		t.Fatal("derived requests duplicated traces or lost settings")
	}
	seen := map[int]bool{}
	for k, index := range info.EventIndices {
		if info.Events[k].Start != events[index].Start || len(info.Events[k].Ridge) != 20 {
			t.Fatal("wrong source index/trace")
		}
		seen[index] = true
	}
	from := 0
	for {
		res, err = http.Get(server.URL + "/api/jobs/" + info.ID + "/fragments?from=" + strconv.Itoa(from) + "&limit=200")
		if err != nil {
			t.Fatal(err)
		}
		var page fragmentPage
		decodeResponse(t, res, 200, &page)
		if len(page.Events) > 200 || page.Total != len(events) || !page.End && page.Next <= from {
			t.Fatal("unbounded or stalled paging")
		}
		for k, index := range page.Indices {
			if seen[index] || page.Events[k].Start != events[index].Start || page.Events[k].Evidence.Level != "weak" || len(page.Events[k].Ridge) != 20 {
				t.Fatal("lost, duplicated or misidentified fragment")
			}
			seen[index] = true
		}
		if page.End {
			break
		}
		from = page.Next
	}
	if len(seen) != len(events) {
		t.Fatal("silently dropped weak fragments")
	}
	// The compact on-disk record must restore full server-side measurements.
	restored, err := New(context.Background(), s.options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(restored.Close)
	restoredServer := httptest.NewServer(restored)
	t.Cleanup(restoredServer.Close)
	rj := restored.jobs[info.ID]
	if rj == nil || len(rj.snapshot().Events) != len(events) || rj.snapshot().Events[1001].Ridge[4] != events[1001].Ridge[4] {
		t.Fatal("restart lost full acoustic evidence")
	}
	res, err = http.Get(restoredServer.URL + "/api/jobs/" + info.ID + "/fragments?from=1001&limit=200")
	if err != nil {
		t.Fatal(err)
	}
	var page fragmentPage
	decodeResponse(t, res, 200, &page)
	if len(page.Events) == 0 || page.Indices[0] != 1001 {
		t.Fatal("restored index mapping changed")
	}
}
