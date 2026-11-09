package webapp

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pinpoint/internal/acoustic"
	"pinpoint/internal/geo"
	"pinpoint/internal/search"
	"pinpoint/internal/tracks"
)

func TestAutomaticWebGroupsPublishOverviewControlsAndCurveMaps(t *testing.T) {
	s, server := testServer(t)
	j, err := s.newJob("mixed-jet-propeller.wav")
	if err != nil {
		t.Fatal(err)
	}
	c := search.DefaultConfig()
	c.RecordingStart = "2023-11-14T22:13:20Z"
	c.ClockUncertainty = 1
	c.ClockStep = .5
	c.Grid = 200
	c.DenseGrid = 100
	c.Refine = 0
	c.Staged = true
	h := 0.
	c.ObserverHeight = &h
	c.Bounds = geo.Bounds{South: -.0005, North: .0005, West: -.0005, East: .0005}
	sp := &acoustic.Spectrum{Version: 1, SampleRate: 1024, Window: 1024, Hop: 1024, FirstBin: 40, Rows: make([][]float32, 150)}
	for i := range sp.Rows {
		sp.Rows[i] = make([]float32, 800)
	}
	root := geo.ECEF(0, 0, 0)
	var ts []tracks.Track
	for n := 0; n < 2; n++ {
		center, tone := 20+80*float64(n), 130+40*float64(n)
		tr := tracks.Track{ID: []string{"jet", "propeller"}[n], TypeCode: []string{"A320", "C172"}[n]}
		for sec := -1000.; sec <= 1000; sec += 2 {
			p := root.Add(geo.Vec{X: 1200, Y: 80 * (sec - center)})
			if n == 1 {
				p = root.Add(geo.Vec{X: 1200, Z: 80 * (sec - center)})
			}
			la, lo, alt := geo.Geodetic(p)
			tr.Points = append(tr.Points, tracks.Point{Time: 1700000000 + sec, Lat: la, Lon: lo, Alt: alt, Position: p})
		}
		ts = append(ts, tr)
		e := acoustic.Event{Start: center - 14.5, End: center + 15.5, CPATime: center, Partial: true, FMin: tone * .8, FMax: tone * 1.2, FStep: .25, Harmonics: []int{1}, MinSpeed: 15, MaxSpeed: 300, MaxSlant: 15000, Evidence: &acoustic.CurveEvidence{Level: "strong", Predictive: true}, Measurement: &acoustic.RidgeMeasurement{ShapePass: true}}
		for i, row := range sp.Rows {
			at := sp.Time(i)
			if at < e.Start || at > e.End {
				continue
			}
			u := at - center
			const sound = 343.
			te := (sound*sound*u - math.Sqrt(sound*sound*80*80*u*u+(sound*sound-80*80)*1200*1200)) / (sound*sound - 80*80)
			hz := tone * sound / (sound + 80*80*te/math.Hypot(1200, 80*te))
			e.Ridge = append(e.Ridge, acoustic.RidgeSample{Time: at, Hz: hz})
			for k := range row {
				row[k] += float32(25 * math.Exp(-math.Pow((sp.Hz(k)-hz)/.8, 2)))
			}
		}
		c.Events = append(c.Events, e)
	}
	extra := c.Events[0]
	extra.Start = 125.5
	extra.End = 145.5
	extra.CPATime = 135.5
	extra.FMin = 500
	extra.FMax = 700
	extra.Ridge = nil
	for at := 126.; at <= 145; at++ {
		extra.Ridge = append(extra.Ridge, acoustic.RidgeSample{Time: at, Hz: 650 - at})
		row := sp.Rows[int(at)]
		for k := range row {
			row[k] += float32(25 * math.Exp(-math.Pow((sp.Hz(k)-(650-at))/.8, 2)))
		}
	}
	c.Events = append(c.Events, extra)
	if err = s.findGroups(context.Background(), j, c, sp, ts); err != nil {
		t.Fatal(err)
	}
	info := j.snapshot()
	if info.ResultStatus != "candidate_sequence" || !strings.HasSuffix(info.ResultURL, "/automatic.html") || len(info.EventMaps) == 0 {
		t.Fatalf("missing automatic workflow: %+v", info)
	}
	for _, path := range []string{info.ResultURL, strings.Replace(info.ResultURL, "automatic.html", "automatic.json", 1), info.EventMaps[0].URL, strings.Replace(info.EventMaps[0].URL, "map.html", "evidence.json", 1)} {
		res, err := http.Get(server.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != 200 {
			t.Fatalf("inaccessible group artifact: %s %s", path, body)
		}
	}
	var report search.AutomaticReport
	data, err := os.ReadFile(filepath.Join(j.dir, "automatic.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &report); err != nil {
		t.Fatal(err)
	}
	if len(report.Results) != 9 || report.Best == nil || report.Best.Events != [2]int{0, 1} {
		b, _ := json.Marshal(report)
		t.Fatal("UI lost all-pair/global-control selection", string(b))
	}
}
