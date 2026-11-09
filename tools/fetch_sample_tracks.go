//go:build ignore

// Fetch state-wide discovery indexes and geometric traces, never a GPS-centred
// shortlist. A separate closed-day cache avoids old partially fetched traces.
package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"time"

	"pinpoint/internal/geo"
	"pinpoint/internal/tracks"
)

type recording struct {
	ID        string  `json:"id"`
	Audio     string  `json:"audio"`
	Family    string  `json:"family"`
	Duration  float64 `json:"duration_s"`
	Status    string  `json:"analysis_status"`
	Detection struct {
		Strong int `json:"strong_curves"`
	} `json:"detection"`
}

func main() {
	audit := flag.String("audit", "work/sample-audit-2026-11-07", "audit directory")
	cache := flag.String("cache", "work/sample-audit-data/cache", "fresh closed-date replay and geometric trace cache")
	family := flag.String("family", "personal", "personal Texas or pathak California recordings")
	flag.Parse()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	ctx, timeout := context.WithTimeout(ctx, 2*time.Hour)
	defer timeout()
	state := "TX"
	if *family == "pathak" {
		state = "CA"
	} else if *family != "personal" {
		panic("invalid family")
	}
	bounds, _ := geo.StateBounds(state)
	bounds.South -= .3
	bounds.North += .3
	bounds.West -= .3
	bounds.East += .3
	zone, err := time.LoadLocation("America/Chicago")
	must(err)
	pattern := regexp.MustCompile(`20\d\d_\d\d_\d\d_\d\d_\d\d_\d\d`)
	done := map[string]bool{}
	for {
		b, err := os.ReadFile(filepath.Join(*audit, "report.json"))
		must(err)
		var report struct {
			Recordings []recording `json:"recordings"`
		}
		must(json.Unmarshal(b, &report))
		pending := false
		for _, r := range report.Recordings {
			if r.Family != *family || done[r.ID] {
				continue
			}
			if r.Status != "complete" {
				pending = true
				continue
			}
			done[r.ID] = true
			if r.Detection.Strong == 0 {
				fmt.Println(r.ID, "no usable curves; no flight download required")
				continue
			}
			dir := filepath.Join(*audit, r.ID, "statewide-tracks")
			must(os.MkdirAll(dir, 0700))
			path := filepath.Join(dir, "tracks.csv")
			if _, err := os.Stat(path); err == nil {
				fmt.Println(r.ID, "existing statewide tracks")
				continue
			}
			var base time.Time
			o := tracks.ImportOptions{MaxGap: 30, Bounds: &bounds}
			if *family == "personal" {
				base, err = time.ParseInLocation("2006_01_02_15_04_05", pattern.FindString(r.Audio), zone)
				must(err)
				start := float64(base.Unix())
				o.Start, o.End = start-1710, start+r.Duration+1710
			} else {
				f, err := os.Open(filepath.Join(filepath.Dir(r.Audio), "frequency_vs_time_used.csv"))
				must(err)
				rows, err := csv.NewReader(f).ReadAll()
				f.Close()
				must(err)
				base, err = time.Parse(time.RFC3339Nano, rows[1][0])
				must(err)
				last, err := time.Parse(time.RFC3339Nano, rows[len(rows)-1][0])
				must(err)
				o.Start, o.End = float64(base.Unix())-300, float64(last.Unix())+300
			}
			f, err := os.Create(path + ".partial")
			must(err)
			var stats tracks.ReplayStats
			source := "closed-day replay indexes and geometric full traces"
			parts, _ := filepath.Glob(filepath.Join("data", base.UTC().Format("2006-01-02"), "*.tar.*"))
			if *family == "pathak" && len(parts) > 0 {
				fmt.Println(r.ID, "Importing complete local daily archive", parts)
				source = "complete local daily archive"
				stats.Import, err = tracks.ImportTarArchives(ctx, [][]string{parts}, f, o)
			} else {
				stats, err = tracks.Replay(ctx, *cache, f, o, 8e9, func(stage string, done, total int) {
					if total == 0 || done%100 == 0 || done == total {
						fmt.Println(r.ID, stage, done, total)
					}
				})
			}
			f.Close()
			message := ""
			if err != nil {
				message = err.Error()
			}
			b, _ = json.MarshalIndent(map[string]any{"scope": "Complete state bounding box plus 0.3 degree discovery padding; receiver GPS is not input", "source": source, "state": state, "discovery_bounds": bounds, "recording_or_first_publisher_measurement_time": base, "from_utc": time.Unix(int64(o.Start), 0).UTC(), "until_utc": time.Unix(int64(o.End), 0).UTC(), "cache": *cache, "stats": stats, "error": message, "fetched_utc": time.Now().UTC(), "note": "Replay indexes discover aircraft; only geometric altitude in full trajectories enters acoustic modeling. Missing traces are retained as a coverage limitation."}, "", "  ")
			must(os.WriteFile(filepath.Join(dir, "fetch.json"), append(b, '\n'), 0600))
			if err != nil {
				fmt.Println(r.ID, "ERROR", err)
				continue
			}
			must(os.Rename(path+".partial", path))
			fmt.Println(r.ID, "DONE", stats.Aircraft, stats.Downloaded, stats.Import.Points)
		}
		if !pending {
			break
		}
		select {
		case <-ctx.Done():
			panic(ctx.Err())
		case <-time.After(10 * time.Second):
		}
	}
}
func must(err error) {
	if err != nil {
		panic(err)
	}
}
