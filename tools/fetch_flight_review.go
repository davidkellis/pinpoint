//go:build ignore

// Acquire geometric flight tracks for explicitly configured recording windows.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"pinpoint/internal/geo"
	"pinpoint/internal/tracks"
)

func check(err error) {
	if err != nil {
		panic(err)
	}
}

func main() {
	config := flag.String("config", "", "recording windows and optional receiver-centred discovery bounds")
	out := flag.String("out", "work/flight-history-review", "output root")
	cache := flag.String("cache", "work/flight-history-cache", "raw replay and geometric trace cache")
	state := flag.String("state", "", "optional whole-state discovery, e.g. TX; otherwise regional diagnostic")
	padding := flag.Float64("padding", 1710, "trajectory padding on each side, seconds")
	limit := flag.Float64("max-download-gb", 2, "maximum download per recording in decimal GB")
	flag.Parse()
	if *config == "" || !geo.Finite(*padding) || *padding < 0 || *padding > 3600 || !geo.Finite(*limit) || *limit <= 0 || *limit > 20 {
		panic("supply --config; padding must be 0–3600 s and download limit 0–20 GB")
	}
	data, err := os.ReadFile(*config)
	check(err)
	var c struct {
		Lat        float64 `json:"lat"`
		Lon        float64 `json:"lon"`
		Recordings []struct {
			ID       string  `json:"id"`
			Start    string  `json:"start"`
			Duration float64 `json:"duration_s"`
		} `json:"recordings"`
	}
	check(json.Unmarshal(data, &c))
	if len(c.Recordings) == 0 {
		panic("no recording windows")
	}
	bounds := geo.Bounds{South: c.Lat - .5, North: c.Lat + .5, West: c.Lon - .6, East: c.Lon + .6}
	scope := "Supplied receiver region; diagnostic acquisition, not a blind state-wide search"
	if *state != "" {
		var ok bool
		bounds, ok = geo.StateBounds(*state)
		if !ok {
			panic("unknown state")
		}
		bounds.South -= .3
		bounds.North += .3
		bounds.West -= .3
		bounds.East += .3
		scope = "Whole-state discovery with 0.3 degree boundary padding"
	}
	if !bounds.Valid() || !geo.Finite(c.Lat) || !geo.Finite(c.Lon) || math.Abs(c.Lat) > 85 || math.Abs(c.Lon) > 180 {
		panic("invalid discovery bounds")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	ctx, timeout := context.WithTimeout(ctx, 45*time.Minute)
	defer timeout()
	failed := false
	for _, r := range c.Recordings {
		if r.ID == "" || filepath.Base(r.ID) != r.ID || !geo.Finite(r.Duration) || r.Duration <= 0 {
			panic("invalid recording")
		}
		base, err := time.Parse(time.RFC3339Nano, r.Start)
		check(err)
		epoch := float64(base.UnixNano()) / 1e9
		o := tracks.ImportOptions{Start: epoch - *padding, End: epoch + r.Duration + *padding, MaxGap: 30, Bounds: &bounds}
		if o.End > float64(time.Now().Unix())-60 {
			panic("requested padded window has not closed; reduce --padding or retry later")
		}
		dir := filepath.Join(*out, r.ID)
		check(os.MkdirAll(dir, 0700))
		path := filepath.Join(dir, "tracks.csv")
		keyBytes, err := json.Marshal(struct {
			Options tracks.ImportOptions
			Cache   string
		}{o, *cache})
		check(err)
		key := sha256.Sum256(keyBytes)
		keyText := hex.EncodeToString(key[:])
		if b, e := os.ReadFile(filepath.Join(dir, "fetch.json")); e == nil {
			var old struct {
				Key   string `json:"acquisition_key"`
				Error string `json:"error"`
			}
			if json.Unmarshal(b, &old) == nil && old.Key == keyText && old.Error == "" {
				if _, e = os.Stat(path); e == nil {
					fmt.Println(r.ID, "reusing completed acquisition")
					continue
				}
			}
		}
		f, err := os.Create(path + ".partial")
		check(err)
		last := time.Time{}
		stats, fetchErr := tracks.Replay(ctx, *cache, f, o, int64(*limit*1e9), func(stage string, done, total int) {
			if time.Since(last) > 10*time.Second || total > 0 && done == total {
				fmt.Printf("%s · %s · %d/%d\n", r.ID, stage, done, total)
				last = time.Now()
			}
		})
		closeErr := f.Close()
		if fetchErr == nil {
			fetchErr = closeErr
		}
		message := ""
		if fetchErr != nil {
			message = fetchErr.Error()
			failed = true
		}
		meta := map[string]any{"provider": "ADSB.lol", "recording": r, "discovery_scope": scope, "discovery_bounds": bounds, "options": o, "acquisition_key": keyText, "cache": *cache, "fetched_utc": time.Now().UTC(), "stats": stats, "error": message, "note": "Replay indexes discover aircraft. Only full-trace geometric altitude enters acoustic geometry. Missing traces, regional discovery and current-day source coverage remain explicit limitations."}
		b, err := json.MarshalIndent(meta, "", "  ")
		check(err)
		check(os.WriteFile(filepath.Join(dir, "fetch.json"), b, 0600))
		if fetchErr != nil {
			fmt.Println(r.ID, "failed:", message)
			continue
		}
		check(os.Rename(path+".partial", path))
		fmt.Printf("%s · %d discovered / %d traces / %d geometric points\n", r.ID, stats.Aircraft, stats.Downloaded, stats.Import.Points)
	}
	if failed {
		os.Exit(1)
	}
}
