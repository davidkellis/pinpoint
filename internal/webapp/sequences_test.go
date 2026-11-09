package webapp

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"pinpoint/internal/acoustic"
	"pinpoint/internal/geo"
	"pinpoint/internal/search"
	"pinpoint/internal/tracks"
)

func TestWebSequencesPublishControlsAndRejectCountingOneAircraftTwice(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg required")
	}
	s, server := testServer(t)
	j, err := s.newJob("one-flyover.wav")
	if err != nil {
		t.Fatal(err)
	}
	audio, ts := recordingFixture(t)
	path := filepath.Join(j.dir, "audio")
	if err = os.WriteFile(path, audio, 0600); err != nil {
		t.Fatal(err)
	}
	sp, err := acoustic.Analyze(context.Background(), path, acoustic.Defaults())
	if err != nil {
		t.Fatal(err)
	}
	c := search.DefaultConfig()
	c.RecordingStart = "2023-11-14T22:13:20Z"
	c.ClockUncertainty = 1
	c.ClockStep = .5
	c.Grid, c.Refine, c.FinalGrid = 200, 0, 0
	c.Bounds = geo.Bounds{South: -.0005, North: .0005, West: -.0005, East: .0005}
	h := 0.
	c.ObserverHeight = &h
	// Complete control coverage; the same observed flight is deliberately
	// proposed as two separate curves. It must not become two airplanes.
	ts[0].Points = nil
	for sec := -2000.; sec <= 2000; sec += 2 {
		p := geo.ECEF(0, 0, 0).Add(geo.Vec{X: 1200, Y: (sec - 20) * 80})
		la, lo, alt := geo.Geodetic(p)
		ts[0].Points = append(ts[0].Points, tracks.Point{Time: 1700000000 + sec, Lat: la, Lon: lo, Alt: alt, Position: p})
	}
	for _, from := range []float64{1.5, 20.5} {
		e := acoustic.Event{Start: from, End: from + 18, Resolution: 1, CPATime: from + 9, Partial: true, FMin: 80, FMax: 180, FStep: .25, Harmonics: []int{1}, MinSpeed: 15, MaxSpeed: 300, MaxSlant: 15000, Evidence: &acoustic.CurveEvidence{Level: "strong", Predictive: true}, Measurement: &acoustic.RidgeMeasurement{ShapePass: true}}
		for at := e.Start; at <= e.End; at += .25 {
			u := at - 20
			te := (343*343*u - math.Sqrt(343*343*80*80*u*u+(343*343-80*80)*1200*1200)) / (343*343 - 80*80)
			hz := 130 * 343 / (343 + 80*80*te/math.Hypot(1200, 80*te))
			e.Ridge = append(e.Ridge, acoustic.RidgeSample{Time: at, Hz: hz})
		}
		c.Events = append(c.Events, e)
	}
	if err = s.findSequences(context.Background(), j, c, sp, ts); err != nil {
		t.Fatal(err)
	}
	info := j.snapshot()
	if info.State != "complete" || info.ResultStatus != "no_match" || len(info.EventMaps) != 0 {
		t.Fatalf("duplicated one aircraft: %+v", info)
	}
	for _, path := range []string{info.ResultURL, strings.Replace(info.ResultURL, "automatic.html", "automatic.json", 1)} {
		res, err := http.Get(server.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("sequence artifact unavailable: %s %s", path, body)
		}
	}
	var report search.SequenceReport
	data, err := os.ReadFile(filepath.Join(j.dir, "automatic.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &report); err != nil {
		t.Fatal(err)
	}
	if len(report.Groups) != 9 || len(report.Options.ControlShifts) != 8 || report.Groups[0].Best != nil || len(report.Groups[0].Reasons) < 2 {
		t.Fatal("missing full controls or diagnostic rejection counts")
	}
}
