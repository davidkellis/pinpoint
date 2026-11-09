package webapp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"pinpoint/internal/acoustic"
	"pinpoint/internal/search"
	"pinpoint/internal/tracks"
)

// Opt in with an absolute path to a saved, completed small-area independent
// search. Replays its first four events with identical inputs and worker budget.
// All new results live in the benchmark's temporary directory.
func BenchmarkIndependentEvents(b *testing.B) {
	dir := os.Getenv("PINPOINT_BENCH_JOB")
	if dir == "" {
		b.Skip("set PINPOINT_BENCH_JOB to a completed small-area job directory")
	}
	var c search.Config
	raw, err := os.ReadFile(filepath.Join(dir, "event-01", "search.json"))
	if err != nil {
		b.Fatal(err)
	}
	if err = json.Unmarshal(raw, &c); err != nil {
		b.Fatal(err)
	}
	spectrum, err := acoustic.Load(c.Spectrum)
	if err != nil {
		b.Fatal(err)
	}
	f, err := os.Open(c.Tracks)
	if err != nil {
		b.Fatal(err)
	}
	ts, err := tracks.Read(f)
	f.Close()
	if err != nil {
		b.Fatal(err)
	}
	r := SearchRequest{Automatic: true, Moving: true}
	for k := 1; k <= 4; k++ {
		var one search.Config
		raw, err := os.ReadFile(filepath.Join(dir, fmt.Sprintf("event-%02d", k), "search.json"))
		if err != nil {
			b.Fatal(err)
		}
		if err = json.Unmarshal(raw, &one); err != nil {
			b.Fatal(err)
		}
		r.Events = append(r.Events, one.Events[0])
	}
	c.Events = r.Events
	root := b.TempDir()
	if c.ObserverHeight == nil {
		// Use already-cached terrain, without copying hundreds of MB per run.
		if err = os.Symlink(filepath.Dir(c.TerrainDir), filepath.Join(root, "cache")); err != nil {
			b.Fatal(err)
		}
	}
	s, err := New(context.Background(), Options{DataDir: root, Workers: 8})
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(s.Close)
	c.Workers = 8
	var baseline [][]byte
	for _, n := range []int{1, 2} {
		b.Run(fmt.Sprintf("events=%d/workers=8", n), func(b *testing.B) {
			s.options.EventWorkers = n
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				j, err := s.newJob("benchmark.wav")
				if err != nil {
					b.Fatal(err)
				}
				if err = s.findEvents(context.Background(), j, r, c, spectrum, ts); err != nil {
					b.Fatal(err)
				}
				if j.snapshot().ResultStatus == "partial" {
					b.Fatal("event failed")
				}
				b.StopTimer()
				for k := 1; k <= 4; k++ {
					data, err := os.ReadFile(filepath.Join(j.dir, fmt.Sprintf("event-%02d", k), "results.json"))
					if err != nil {
						b.Fatal(err)
					}
					if len(baseline) < k {
						baseline = append(baseline, data)
					} else if !bytes.Equal(baseline[k-1], data) {
						b.Fatalf("event %d output differs from serial baseline", k)
					}
				}
				b.StartTimer()
			}
		})
	}
}
