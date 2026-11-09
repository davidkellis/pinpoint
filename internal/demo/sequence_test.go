package demo

import (
	"context"
	"encoding/json"
	"math"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"pinpoint/internal/acoustic"
	"pinpoint/internal/geo"
	"pinpoint/internal/search"
	"pinpoint/internal/tracks"
)

// This synthesizes emitted phase from the closed-form retarded-time solution,
// independently of tracks.Received and the search/validation implementations.
// Type codes test eligibility, not realistic engine acoustics or jet accuracy.
func TestThreeFlyoversFromWAVThroughDetectionAndReservedValidation(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg required")
	}
	dir := t.TempDir()
	audio := filepath.Join(dir, "three-flights.wav")
	const rate = 4096
	const duration = 320
	const sound = 343.
	root := geo.ECEF(Lat, Lon, Height)
	velocities := []geo.Vec{{Y: 85}, {Z: 95}, {Y: 60, Z: 80}}
	centers := []float64{40, 160, 280}
	tones := []float64{130, 170, 210}
	var ts []tracks.Track
	for k, v := range velocities {
		tr := tracks.Track{ID: []string{"aircraft-one", "aircraft-two", "aircraft-three"}[k], TypeCode: []string{"C172", "B738", "E55P"}[k]}
		for sec := -800.; sec <= 1000; sec += 2 {
			p := root.Add(geo.Vec{X: 1300}).Add(v.Scale(sec - centers[k]))
			la, lo, h := geo.Geodetic(p)
			tr.Points = append(tr.Points, tracks.Point{Time: Epoch + sec, Lat: la, Lon: lo, Alt: h, Position: p})
		}
		ts = append(ts, tr)
	}
	pcm := make([]int16, rate*duration)
	rng := rand.New(rand.NewSource(21))
	for i := range pcm {
		at := float64(i) / rate
		value := .008 * rng.NormFloat64()
		for k, v := range velocities {
			u := at - centers[k]
			v2 := v.Dot(v)
			te := (sound*sound*u - math.Sqrt(sound*sound*v2*u*u+(sound*sound-v2)*1300*1300)) / (sound*sound - v2)
			env := math.Exp(-math.Pow(te/26, 8))
			value += env * (.22*math.Sin(2*math.Pi*tones[k]*te) + .06*math.Sin(4*math.Pi*tones[k]*te))
		}
		pcm[i] = int16(math.Max(-1, math.Min(1, value)) * 32767)
	}
	f, err := os.Create(audio)
	if err != nil {
		t.Fatal(err)
	}
	err = writeWAV(f, pcm, rate)
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	s, d, err := acoustic.AnalyzeMultiResolution(context.Background(), audio)
	if err != nil {
		t.Fatal(err)
	}
	c := search.DefaultConfig()
	c.RecordingStart = time.Unix(int64(Epoch), 0).UTC().Format(time.RFC3339)
	c.Events = acoustic.SearchEvents(d.Events, true, false)
	if len(c.Events) < 3 {
		t.Fatalf("synthetic source detector lost flyovers: %d usable curves", len(c.Events))
	}
	c.Bounds = geo.Bounds{South: Lat - .002, North: Lat + .002, West: Lon - .003, East: Lon + .003}
	h := Height
	c.ObserverHeight = &h
	c.Grid = 200
	c.Refine = 50
	c.FinalGrid = 10
	c.ClockUncertainty = 2
	c.ClockStep = .5
	c.Workers = 3
	o := search.DefaultSequenceOptions(c)
	o.AudioPath = audio
	o.CacheDir = filepath.Join(dir, "cache")
	o.ControlShifts = []float64{-400, 400}
	r, err := search.RunAutomaticSequences(context.Background(), c, s, ts, o, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != "candidate_sequence" || r.Selected == nil {
		b, _ := json.MarshalIndent(r, "", "  ")
		t.Fatalf("raw three-flyover validation failed: %s", b)
	}
	g := r.Groups[*r.Selected]
	if len(g.Indices) != 3 || !g.Pass || geo.ECEF(g.Best.Lat, g.Best.Lon, g.Best.Height).Sub(root).Norm() > 180 {
		t.Fatalf("wrong raw-audio sequence: %+v", g)
	}
	// A validation-only update must bind the original data and binary while
	// retaining every training winner, including wrong-time trials.
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	rechecked, err := search.RevalidateSequences(context.Background(), data, binary, audio, filepath.Join(dir, "revalidation"), s, ts)
	if err != nil || rechecked.Revalidation == nil || rechecked.Status != r.Status || !reflect.DeepEqual(rechecked.Selected, r.Selected) {
		t.Fatalf("failed frozen revalidation: %s %v", rechecked.Status, err)
	}
	for i := range r.Groups {
		if !reflect.DeepEqual(r.Groups[i].Best, rechecked.Groups[i].Best) {
			t.Fatal("reserved audio altered training parameters")
		}
	}
	s.Rows[0][0]++
	if _, err = search.RevalidateSequences(context.Background(), data, binary, audio, filepath.Join(dir, "changed"), s, ts); err == nil {
		t.Fatal("revalidation accepted different source measurements")
	}
}
