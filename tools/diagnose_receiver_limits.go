//go:build ignore

// Controlled illustrations of selection/validation limits, not aircraft benchmarks.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"pinpoint/internal/acoustic"
	"pinpoint/internal/geo"
	"pinpoint/internal/search"
	"pinpoint/internal/tracks"
)

func fixture(length int, emissionCPA, amplitude, humHz, humWidth, humAmplitude float64) (*acoustic.Spectrum, []tracks.Track, search.ReceiverHypothesis) {
	const epoch = 1700000000.
	const sound = 343.
	const distance = 1500.
	const speed = 85.
	const source = 160.
	s := &acoustic.Spectrum{Version: 1, Source: "Controlled synthetic tone; not a phone recording", SampleRate: 1024, Window: 1024, Hop: 1024, FirstBin: 40, Rows: make([][]float32, length)}
	arrivalCPA := emissionCPA + distance/sound
	for i := range s.Rows {
		row := make([]float32, 300)
		s.Rows[i] = row
		t := s.Time(i)
		// Closed-form retarded time for a straight constant-speed source.
		u := t - emissionCPA
		te := (sound*sound*u - math.Sqrt(sound*sound*speed*speed*u*u+(sound*sound-speed*speed)*distance*distance)) / (sound*sound - speed*speed)
		frequency := source * sound / (sound + speed*speed*te/math.Sqrt(distance*distance+speed*speed*te*te))
		for k := range row {
			if math.Abs(t-arrivalCPA) <= 20.5 {
				row[k] = float32(amplitude * math.Exp(-math.Pow((s.Hz(k)-frequency)/.8, 2)))
			}
			if humAmplitude > 0 {
				row[k] += float32(humAmplitude * math.Exp(-math.Pow((s.Hz(k)-humHz)/humWidth, 2)))
			}
		}
	}
	obs := geo.ECEF(0, 0, 0)
	track := tracks.Track{ID: "synthetic-aircraft"}
	for sec := -100.; sec <= 900; sec += 2 {
		p := obs.Add(geo.Vec{X: distance, Y: speed * (sec - emissionCPA)})
		lat, lon, height := geo.Geodetic(p)
		track.Points = append(track.Points, tracks.Point{Time: epoch + sec, Lat: lat, Lon: lon, Alt: height, Position: p})
	}
	h := search.ReceiverHypothesis{Lat: 0, Lon: 0, Height: 0, RecordingStart: time.Unix(int64(epoch), 0).UTC().Format(time.RFC3339), SoundSpeed: sound, MaxGap: 30}
	return s, []tracks.Track{track}, h
}

func main() {
	for _, arg := range os.Args[1:] {
		if strings.HasPrefix(arg, "-") && !strings.HasPrefix(arg, "--") {
			panic("use two-hyphen options")
		}
	}
	out := flag.String("out", "work/receiver-limit-diagnosis", "new diagnostic output directory")
	flag.Parse()
	if err := os.MkdirAll(*out, 0700); err != nil {
		panic(err)
	}
	o := search.DefaultGuidedOptions()
	o.MinHz = 60
	o.MaxHz = 220
	o.StepHz = .5
	s, ts, h := fixture(80, 40-1500./343, 25, 0, 1, 0)
	short, err := search.ReviewReceiver(context.Background(), h, s, ts, o)
	if err != nil {
		panic(err)
	}
	result := map[string]any{"note": "Controlled synthetic counterexamples for program logic. The tone is explicitly present and generated with an independent straight-line retarded-time equation. These are not real aircraft recordings, false-negative-rate estimates, or proof of any particular missed plane in the user's files.", "known_source_hz": 160, "perfect_80_second_clip": short}
	s, ts, h = fixture(800, 180, 6, 80, 2, 20)
	broad, err := search.ReviewReceiver(context.Background(), h, s, ts, o)
	if err != nil {
		panic(err)
	}
	result["clutter_fixture"] = map[string]any{"true_curve_peak_db": 6, "steady_hum_hz": 80, "steady_hum_width_hz": 2, "steady_hum_peak_db": 20, "broad_search": broad}
	result["version"] = 2
	result["purpose"] = "Replay the exact historical short-clip and clutter counterexamples without searching for another failure or changing their parameters."
	result["short_clip_recovered"] = short.Supported > 0
	result["clutter_curve_recovered"] = broad.Supported > 0
	b, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		panic(err)
	}
	if err = os.WriteFile(filepath.Join(*out, "diagnosis.json"), b, 0600); err != nil {
		panic(err)
	}
	fmt.Printf("Short fixture: %d matches, %d supported; clutter fixture: %d supported\n", len(short.Matches), short.Supported, broad.Supported)
}
