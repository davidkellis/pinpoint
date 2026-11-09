//go:build ignore

// Align publisher UTC frequency labels to a WAV's measured spectrogram.
// This supplies an inferred clock and evaluation labels, never a location.
package main

import (
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"pinpoint/internal/acoustic"
	"sort"
	"strconv"
	"time"
)

type trial struct {
	Offset float64 `json:"offset_s"`
	Score  float64 `json:"mean_spectral_contrast_db"`
}

func main() {
	spec := flag.String("spectrum", "", "measured Go spectrogram.bin")
	source := flag.String("csv", "", "publisher time_utc,frequency_hz CSV")
	out := flag.String("out", "", "output directory")
	flag.Parse()
	if *spec == "" || *source == "" || *out == "" {
		panic("required: --spectrum, --csv, --out")
	}
	s, err := acoustic.Load(*spec)
	must(err)
	f, err := os.Open(*source)
	must(err)
	rows, err := csv.NewReader(f).ReadAll()
	f.Close()
	must(err)
	if len(rows) < 3 || len(rows[0]) != 2 || rows[0][0] != "time_utc" || rows[0][1] != "frequency_hz" {
		panic("unexpected frequency label CSV")
	}
	var first time.Time
	points := make([]acoustic.RidgeSample, 0, len(rows)-1)
	for i, row := range rows[1:] {
		when, err := time.Parse(time.RFC3339Nano, row[0])
		must(err)
		hz, err := strconv.ParseFloat(row[1], 64)
		must(err)
		if i == 0 {
			first = when
		}
		rel := when.Sub(first).Seconds()
		if !finite(hz) || hz <= 0 || len(points) > 0 && rel <= points[len(points)-1].Time {
			panic("invalid or unordered labels")
		}
		points = append(points, acoustic.RidgeSample{Time: rel, Hz: hz})
	}
	var trials []trial
	maxOffset := s.Time(len(s.Rows)-1) - points[len(points)-1].Time
	for offset := s.Time(0); offset <= maxOffset; offset += .025 {
		sum := 0.0
		ok := true
		for _, p := range points {
			x := (offset + p.Time - s.Time(0)) / s.Step()
			i := int(math.Floor(x))
			alpha := x - float64(i)
			a, ao := s.Value(i, p.Hz)
			b, bo := s.Value(i+1, p.Hz)
			if !ao || !bo {
				ok = false
				break
			}
			sum += (1-alpha)*a + alpha*b
		}
		if ok {
			trials = append(trials, trial{offset, sum / float64(len(points))})
		}
	}
	if len(trials) == 0 {
		panic("frequency labels do not fit inside the measured audio")
	}
	sort.SliceStable(trials, func(i, j int) bool { return trials[i].Score > trials[j].Score })
	best := trials[0]
	for i := range points {
		points[i].Time += best.Offset
	}
	start := first.Add(-time.Duration(math.Round(best.Offset * 1e9)))
	must(os.MkdirAll(*out, 0755))
	write(filepath.Join(*out, "anchors.json"), points)
	write(filepath.Join(*out, "alignment.json"), map[string]any{
		"method":     "Maximize mean measured whitened spectral power along the publisher frequency labels; 0.025 s offset sweep. No flight track or geography used.",
		"source_csv": *source, "source_spectrum": *spec, "label_count": len(points),
		"first_csv_utc": first, "first_csv_offset_in_wav_s": best.Offset,
		"inferred_wav_start_utc": start.Format(time.RFC3339Nano), "clock_uncertainty_for_search_s": 5,
		"top_alignments": trials[:min(10, len(trials))],
		"note":           "Inferred audio clock, not independent verification of UTC. Frequency labels must not be used to select automatic detections in blind evaluations.",
	})
	fmt.Printf("%d labels; offset %.3f s; mean contrast %.3f dB; inferred start %s\n", len(points), best.Offset, best.Score, start.Format(time.RFC3339Nano))
}
func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
func must(err error) {
	if err != nil {
		panic(err)
	}
}
func write(path string, v any) {
	raw, err := json.MarshalIndent(v, "", "  ")
	must(err)
	must(os.WriteFile(path, append(raw, '\n'), 0644))
}
