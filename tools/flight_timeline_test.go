//go:build ignore

package main

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"

	"pinpoint/internal/geo"
	"pinpoint/internal/tracks"
)

func TestTimelineRetardedCPAAndIdentityDeduplication(t *testing.T) {
	obs := geo.ECEF(0, 0, 0)
	track := tracks.Track{ID: "abcdef-1000.000-0", Points: []tracks.Point{
		{Time: 1000, Position: obs.Add(geo.Vec{X: 1000, Y: -1000})},
		{Time: 1020, Position: obs.Add(geo.Vec{X: 1000, Y: 1000})},
	}}
	duplicate := track
	duplicate.ID = "abcdef-1000.000-1"
	ps := timelinePasses([]tracks.Track{track, duplicate}, obs, 1000, 30, 5000, 343)
	if len(ps) != 1 || math.Abs(ps[0].CPA-(10+1000./343)) > 1e-6 || ps[0].Partial {
		t.Fatalf("expected one delayed complete encounter: %+v", ps)
	}
	track.Points[1].Time = 1040
	if len(timelinePasses([]tracks.Track{track}, obs, 1000, 60, 5000, 343)) != 0 {
		t.Fatal("a long telemetry gap must not create a synthetic pass")
	}
}

func TestHistoryUsesIndividualHistoricalTimestamp(t *testing.T) {
	dir := t.TempDir()
	os.Mkdir(filepath.Join(dir, "fr24"), 0700)
	track := tracks.Track{ID: "abcdef-1000.000-0", TypeCode: "B738", Points: []tracks.Point{
		{Time: 1000, Lat: 29, Lon: -98.001}, {Time: 1020, Lat: 29, Lon: -97.999},
	}}
	// A global server timestamp can be live even during historical playback.
	// It must neither disqualify a historical aircraft nor admit a live one.
	snapshot := map[string]any{"status": "captured", "requested_timestamp_s": 1010, "playback_timestamp_s": 1010,
		"server_timestamp": 9999999999, "aircraft": []map[string]any{
			{"id": "fr24-leg", "callsign": "TEST1", "latitude": 29, "longitude": -98, "timestamp": 1010},
			{"id": "live", "latitude": 29, "longitude": -98, "timestamp": 9999999999},
		}}
	timelineJSON(filepath.Join(dir, "fr24", "1010.json"), snapshot)
	historyCompare(dir, []tracks.Track{track}, []timelinePass{{ICAO: "abcdef", Callsign: "TEST1", CPA: 10}}, 1000, 30)
	b, e := os.ReadFile(filepath.Join(dir, "fr24-comparison.json"))
	if e != nil {
		t.Fatal(e)
	}
	var report struct {
		Observations []historyJoin `json:"observations"`
	}
	if e = json.Unmarshal(b, &report); e != nil {
		t.Fatal(e)
	}
	if len(report.Observations) != 1 || report.Observations[0].Status != "consistent_callsign_and_position" {
		t.Fatalf("historical observation was confused with live data: %+v", report)
	}
}

func TestHistoryRejectsUnverifiedAndAmbiguousAssignments(t *testing.T) {
	dir := t.TempDir()
	os.Mkdir(filepath.Join(dir, "fr24"), 0700)
	track := tracks.Track{ID: "abcdef-1000.000-0", Points: []tracks.Point{{Time: 1000, Lat: 29, Lon: -98}, {Time: 1020, Lat: 29, Lon: -98}}}
	other := track
	other.ID = "123456-1000.000-0"
	timelineJSON(filepath.Join(dir, "fr24", "1010.json"), map[string]any{
		"status": "captured", "requested_timestamp_s": 1010, "playback_timestamp_s": 1010,
		"aircraft": []map[string]any{{"id": "opaque", "latitude": 29, "longitude": -98, "timestamp": 1010}},
	})
	timelineJSON(filepath.Join(dir, "fr24", "1012.json"), map[string]any{
		"status": "unverified_response_time", "requested_timestamp_s": 1012, "playback_timestamp_s": 1012,
		"aircraft": []map[string]any{{"id": "unverified", "latitude": 29, "longitude": -98, "timestamp": 1012}},
	})
	historyCompare(dir, []tracks.Track{track, other}, nil, 1000, 30)
	b, e := os.ReadFile(filepath.Join(dir, "fr24-comparison.json"))
	if e != nil {
		t.Fatal(e)
	}
	var report struct {
		Observations []historyJoin `json:"observations"`
	}
	if e = json.Unmarshal(b, &report); e != nil {
		t.Fatal(e)
	}
	if len(report.Observations) != 1 || report.Observations[0].Status != "ambiguous_position" {
		t.Fatalf("ambiguous or unverified evidence was promoted: %+v", report)
	}
}

func TestMetadataDoesNotBorrowCallsignFromAnotherFlight(t *testing.T) {
	cache := t.TempDir()
	dir := filepath.Join(cache, "traces", "1970-01-01")
	if e := os.MkdirAll(dir, 0700); e != nil {
		t.Fatal(e)
	}
	timelineJSON(filepath.Join(dir, "abcdef.json"), map[string]any{
		"timestamp": 1000, "r": "N1", "trace": []any{
			[]any{0, 0, 0, 0, 0, 0, 0, 0, map[string]any{"flight": "FIRST"}},
			[]any{3000, 0, 0, 0, 0, 0, 0, 0, map[string]any{"flight": "LATER"}},
		},
	})
	p := timelinePass{ICAO: "abcdef", CPA: 1010, Delay: 5}
	timelineMetadata(&p, 0, cache)
	if p.Callsign != "FIRST" || p.Registration != "N1" {
		t.Fatalf("wrong time-local metadata: %+v", p)
	}
	p = timelinePass{ICAO: "abcdef", CPA: 2000, Delay: 5}
	timelineMetadata(&p, 0, cache)
	if p.Callsign != "" {
		t.Fatalf("borrowed another flight's callsign: %q", p.Callsign)
	}
}
