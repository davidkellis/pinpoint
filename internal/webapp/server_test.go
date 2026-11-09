package webapp

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"pinpoint/internal/geo"
	"pinpoint/internal/search"
	"pinpoint/internal/tracks"
)

func testServer(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()
	s, err := New(context.Background(), Options{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(s)
	t.Cleanup(func() { server.Close(); s.Close() })
	return s, server
}
func multipartUpload(t *testing.T, url, field, name string, data []byte, fields ...map[string]string) *http.Response {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	p, err := w.CreateFormFile(field, name)
	if err != nil {
		t.Fatal(err)
	}
	p.Write(data)
	for _, group := range fields {
		for k, v := range group {
			if err := w.WriteField(k, v); err != nil {
				t.Fatal(err)
			}
		}
	}
	w.Close()
	response, err := http.Post(url, w.FormDataContentType(), &body)
	if err != nil {
		t.Fatal(err)
	}
	return response
}
func decodeResponse(t *testing.T, response *http.Response, status int, out any) {
	t.Helper()
	defer response.Body.Close()
	if response.StatusCode != status {
		b, _ := io.ReadAll(response.Body)
		t.Fatalf("HTTP %d, expected %d: %s", response.StatusCode, status, b)
	}
	if out != nil {
		if err := json.NewDecoder(response.Body).Decode(out); err != nil {
			t.Fatal(err)
		}
	}
}
func waitJob(t *testing.T, url string, want string) JobInfo {
	t.Helper()
	// Audio analysis and the race detector can overlap other CPU-heavy tests.
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		response, err := http.Get(url)
		if err != nil {
			t.Fatal(err)
		}
		var j JobInfo
		decodeResponse(t, response, 200, &j)
		if j.State == want {
			return j
		}
		if j.State == "error" {
			t.Fatalf("job failed: %s", j.Error)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("job did not reach %s", want)
	return JobInfo{}
}
func startSearch(t *testing.T, url string, request SearchRequest) {
	t.Helper()
	body, _ := json.Marshal(request)
	response, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	decodeResponse(t, response, 202, nil)
}

// Independent constant-velocity acoustic fixture, synthesized from emitted
// phase with the closed-form propagation delay. Small enough for HTTP tests.
func recordingFixture(t *testing.T) ([]byte, []tracks.Track) {
	t.Helper()
	const rate = 4096
	const duration = 40
	const c = 343.0
	const v = 80.0
	const d = 1200.0
	samples := make([]int16, rate*duration)
	for i := range samples {
		heard := float64(i)/rate - 20
		emitted := (c*c*heard - math.Sqrt(c*c*v*v*heard*heard+(c*c-v*v)*d*d)) / (c*c - v*v)
		samples[i] = int16(10000*math.Sin(2*math.Pi*130*emitted) + 500*math.Sin(float64(i)*2.139))
	}
	var wav bytes.Buffer
	wav.WriteString("RIFF")
	binary.Write(&wav, binary.LittleEndian, uint32(36+len(samples)*2))
	wav.WriteString("WAVEfmt ")
	binary.Write(&wav, binary.LittleEndian, uint32(16))
	binary.Write(&wav, binary.LittleEndian, uint16(1))
	binary.Write(&wav, binary.LittleEndian, uint16(1))
	binary.Write(&wav, binary.LittleEndian, uint32(rate))
	binary.Write(&wav, binary.LittleEndian, uint32(rate*2))
	binary.Write(&wav, binary.LittleEndian, uint16(2))
	binary.Write(&wav, binary.LittleEndian, uint16(16))
	wav.WriteString("data")
	binary.Write(&wav, binary.LittleEndian, uint32(len(samples)*2))
	binary.Write(&wav, binary.LittleEndian, samples)
	track := tracks.Track{ID: "fixture-aircraft"}
	origin := geo.ECEF(0, 0, 0)
	for sec := -20; sec <= 60; sec += 2 {
		pos := origin.Add(geo.Vec{X: d, Y: float64(sec-20) * v})
		lat, lon, alt := geo.Geodetic(pos)
		track.Points = append(track.Points, tracks.Point{Time: 1700000000 + float64(sec), Lat: lat, Lon: lon, Alt: alt, Position: pos})
	}
	return wav.Bytes(), []tracks.Track{track}
}
func TestUploadAnalyzeSearchMapAndCachedArchive(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg required")
	}
	s, server := testServer(t)
	wav, ts := recordingFixture(t)
	ts[0].TypeCode = "A320" // Known jets must remain eligible with default settings.
	var info JobInfo
	decodeResponse(t, multipartUpload(t, server.URL+"/api/jobs", "audio", "my recording.wav", wav), 202, &info)
	jobURL := server.URL + "/api/jobs/" + info.ID
	info = waitJob(t, jobURL, "ready")
	if len(info.Events) == 0 {
		t.Fatal("automatic flyover detection failed")
	}
	var csv bytes.Buffer
	if err := tracks.Write(&csv, ts); err != nil {
		t.Fatal(err)
	}
	decodeResponse(t, multipartUpload(t, jobURL+"/tracks", "tracks", "flights.csv", csv.Bytes()), 200, nil)
	h := 0.0
	r := defaultRequest()
	r.RecordingStart = "2023-11-14T22:13:20Z"
	r.ClockUncertainty = 0
	r.Bounds = geo.Bounds{South: -.0001, North: .0001, West: -.0001, East: .0001}
	r.Height = &h
	r.Refine = 0
	r.FinalGrid = 0
	r.MatchMode = "independent"
	r.Source = "upload"
	r.IncludeWeak = true // Ingestion/scoring fixture; not an evidence-classifier test.
	startSearch(t, jobURL+"/search", r)
	info = waitJob(t, jobURL, "complete")
	if info.ResultStatus != "candidate_matches" {
		t.Fatal("uploaded recording and tracks did not match")
	}
	response, err := http.Get(server.URL + info.ResultURL)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != 200 || !bytes.Contains(body, []byte("score_grid")) {
		t.Fatal("missing map report")
	}
	// Evidence remains available both before a match and in the scored report.
	response, err = http.Get(jobURL + "/detected/1")
	if err != nil {
		t.Fatal(err)
	}
	var detected search.Evidence
	decodeResponse(t, response, 200, &detected)
	if len(detected.Events) != 1 || len(detected.Candidates) != 0 || !strings.HasPrefix(detected.Events[0].Spectrum.DataURL, "data:image/png;base64,") {
		t.Fatal("missing or fabricated detection evidence")
	}
	response, err = http.Get(server.URL + strings.Replace(info.ResultURL, "map.html", "evidence.json", 1))
	if err != nil {
		t.Fatal(err)
	}
	var evidence search.Evidence
	decodeResponse(t, response, 200, &evidence)
	if len(evidence.Events) == 0 || len(evidence.Candidates) == 0 || len(evidence.Events[0].Models) == 0 {
		t.Fatal("matched curves unavailable")
	}
	// A synthetic split readsb archive exercises the actual download/cache/tar
	// importer pipeline without downloading a multi-gigabyte real archive.
	var rows [][]any
	for _, p := range ts[0].Points {
		rows = append(rows, []any{p.Time - 1700000000, p.Lat, p.Lon, 1000, 155, 90, 0, 0, nil, "adsb_icao", p.Alt / .3048})
	}
	trace, _ := json.Marshal(map[string]any{"icao": "abc123", "timestamp": 1700000000, "trace": rows})
	var gz bytes.Buffer
	z := gzip.NewWriter(&gz)
	z.Write(trace)
	z.Close()
	var archive bytes.Buffer
	tw := tar.NewWriter(&archive)
	tw.WriteHeader(&tar.Header{Name: "traces/23/trace_full_abc123.json.gz", Mode: 0600, Size: int64(gz.Len())})
	tw.Write(gz.Bytes())
	tw.Close()
	data := archive.Bytes()
	parts := [][]byte{data[:len(data)/2], data[len(data)/2:]}
	var downloads atomic.Int64
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		downloads.Add(1)
		if r.URL.Path == "/aa" {
			w.Write(parts[0])
		} else {
			w.Write(parts[1])
		}
	}))
	defer provider.Close()
	var assets []tracks.Asset
	for i, name := range []string{"aa", "ab"} {
		sum := sha256.Sum256(parts[i])
		assets = append(assets, tracks.Asset{Name: "day.tar." + name, URL: provider.URL + "/" + name, Size: int64(len(parts[i])), Digest: "sha256:" + hex.EncodeToString(sum[:])})
	}
	dir := filepath.Join(s.options.DataDir, "cache", "2023-11-14", "prod-0")
	os.MkdirAll(dir, 0700)
	if err = search.JSON(filepath.Join(dir, "manifest.json"), assets); err != nil {
		t.Fatal(err)
	}
	r.Source = "archive"
	startSearch(t, jobURL+"/search", r)
	info = waitJob(t, jobURL, "complete")
	if info.ResultStatus != "candidate_matches" {
		t.Fatal("automatic archive pipeline did not match")
	}
	if downloads.Load() != 2 {
		t.Fatalf("expected two downloaded parts, got %d", downloads.Load())
	}
	startSearch(t, jobURL+"/search", r)
	waitJob(t, jobURL, "complete")
	if downloads.Load() != 2 {
		t.Fatal("cached archive was downloaded again")
	}
	r.Automatic, r.Moving = true, true
	startSearch(t, jobURL+"/search", r)
	info = waitJob(t, jobURL, "complete")
	if len(info.EventMaps) != len(info.Events) || !strings.Contains(info.ResultURL, "/events/1/map.html") {
		t.Fatal("automatic walking search did not produce independent event maps")
	}
	response, err = http.Get(server.URL + info.ResultURL)
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != 200 || !bytes.Contains(body, []byte("Receiver motion is not fitted")) {
		t.Fatal("walking approximation missing from map")
	}
}
func TestRejectsCrossOriginAndInvalidUploads(t *testing.T) {
	_, server := testServer(t)
	request, _ := http.NewRequest("POST", server.URL+"/api/jobs", strings.NewReader("bad"))
	request.Header.Set("Origin", "https://example.com")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	decodeResponse(t, response, 403, nil)
	decodeResponse(t, multipartUpload(t, server.URL+"/api/jobs", "audio", "empty.wav", nil), 400, nil)
	response, err = http.Get(server.URL + "/api/jobs/unknown/files/map.html")
	if err != nil {
		t.Fatal(err)
	}
	decodeResponse(t, response, 404, nil)
	response, err = http.Get(server.URL + "/assets/leaflet.js")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatal("bundled map library unavailable")
	}
}
func TestQueuedJobCancellation(t *testing.T) {
	s, server := testServer(t)
	s.work <- struct{}{}
	defer func() { <-s.work }()
	var info JobInfo
	decodeResponse(t, multipartUpload(t, server.URL+"/api/jobs", "audio", "queued.wav", []byte("pending")), 202, &info)
	url := server.URL + "/api/jobs/" + info.ID
	response, err := http.Post(url+"/cancel", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	decodeResponse(t, response, 200, nil)
	waitJob(t, url, "cancelled")
}
func TestRestartKeepsCompletedJobsAndMarksInterrupted(t *testing.T) {
	dir := t.TempDir()
	s, err := New(context.Background(), Options{DataDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	j, err := s.newJob("saved.wav")
	if err != nil {
		t.Fatal(err)
	}
	j.update(func(i *JobInfo) { i.State = "searching"; i.Phase = "scoring" })
	id := j.info.ID
	completed, err := s.newJob("complete.wav")
	if err != nil {
		t.Fatal(err)
	}
	completed.update(func(i *JobInfo) { i.State = "complete"; i.ResultURL = "/api/jobs/" + i.ID + "/files/map.html" })
	completedID := completed.info.ID
	s.Close()
	restarted, err := New(context.Background(), Options{DataDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	state := restarted.jobs[id].snapshot()
	if state.State != "error" || !strings.Contains(state.Error, "server stopped") {
		t.Fatalf("bad recovery: %+v", state)
	}
	if saved := restarted.jobs[completedID].snapshot(); saved.State != "complete" || saved.ResultURL == "" {
		t.Fatalf("lost completed job: %+v", saved)
	}
}
func TestInvalidSearchFailsBeforeDownloading(t *testing.T) {
	s, _ := testServer(t)
	j, err := s.newJob("test.wav")
	if err != nil {
		t.Fatal(err)
	}
	r := defaultRequest()
	_, err = s.configuration(j, r, nil)
	if err == nil || !strings.Contains(err.Error(), "at least two strong curves") {
		t.Fatal(fmt.Sprint("expected clear missing-curves error: ", err))
	}
	r.MatchMode = "joint"
	_, err = s.configuration(j, r, nil)
	if err == nil || !strings.Contains(err.Error(), "No flyovers") {
		t.Fatal(fmt.Sprint("expected clear missing-events error: ", err))
	}
}
