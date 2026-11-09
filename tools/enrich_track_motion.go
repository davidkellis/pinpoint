//go:build ignore

// Attach reported motion from cached raw traces, preserving original geometry.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"pinpoint/internal/tracks"
)

type enrichmentStats struct{ Points, Horizontal, Vertical, Missing, GeometryMismatch int }

func motionIdentity(id string) string {
	parts := strings.Split(id, "-")
	if len(parts) == 3 && len(parts[0]) == 6 {
		return strings.ToLower(parts[0])
	}
	return id
}
func attachMotion(old tracks.Track, sources []tracks.Track) (tracks.Track, enrichmentStats) {
	out := old
	out.Motion = append([]tracks.KinematicPoint(nil), old.Motion...)
	stats := enrichmentStats{Points: len(old.Points)}
	type candidate struct {
		p tracks.Point
		m tracks.KinematicPoint
	}
	var available []candidate
	for _, source := range sources {
		if motionIdentity(old.ID) != motionIdentity(source.ID) || old.TypeCode != "" && source.TypeCode != "" && old.TypeCode != source.TypeCode {
			continue
		}
		motion := map[float64]tracks.KinematicPoint{}
		for _, m := range source.Motion {
			motion[m.Time] = m
		}
		for _, p := range source.Points {
			if m, ok := motion[p.Time]; ok {
				available = append(available, candidate{p, m})
			}
		}
	}
	sort.SliceStable(available, func(i, j int) bool { return available[i].p.Time < available[j].p.Time })
	merged := map[float64]tracks.KinematicPoint{}
	for _, m := range out.Motion {
		merged[m.Time] = m
	}
	for _, p := range old.Points {
		i := sort.Search(len(available), func(i int) bool { return available[i].p.Time >= p.Time-.005 })
		found := false
		mismatch := false
		for ; i < len(available) && available[i].p.Time <= p.Time+.005; i++ {
			q := available[i]
			if q.p.Position.Sub(p.Position).Norm() > 1 {
				mismatch = true
				continue
			}
			m := q.m
			m.Time = p.Time
			merged[p.Time] = m
			found = true
			if m.Horizontal {
				stats.Horizontal++
			}
			if m.Vertical {
				stats.Vertical++
			}
			break
		}
		if !found {
			stats.Missing++
			if mismatch {
				stats.GeometryMismatch++
			}
		}
	}
	out.Motion = nil
	for _, p := range old.Points {
		if m, ok := merged[p.Time]; ok {
			out.Motion = append(out.Motion, m)
		}
	}
	return out, stats
}
func requireMotion(e error) {
	if e != nil {
		panic(e)
	}
}
func motionSHA(path string) string {
	f, e := os.Open(path)
	requireMotion(e)
	defer f.Close()
	h := sha256.New()
	_, e = io.Copy(h, f)
	requireMotion(e)
	return hex.EncodeToString(h.Sum(nil))
}
func motionSave(path string, v any) {
	b, e := json.MarshalIndent(v, "", "  ")
	requireMotion(e)
	requireMotion(os.WriteFile(path, append(b, '\n'), 0600))
}
func main() {
	for _, a := range os.Args[1:] {
		if strings.HasPrefix(a, "-") && !strings.HasPrefix(a, "--") {
			panic("use two-hyphen options")
		}
	}
	config := flag.String("config", "", "recordings with original track CSV paths")
	cache := flag.String("cache-root", "work/sample-audit-data/cache/traces", "cached raw trace root with UTC date subdirectories")
	outDir := flag.String("out", "work/track-motion-enrichment", "new output directory")
	flag.Parse()
	if *config == "" {
		panic("supply --config")
	}
	b, e := os.ReadFile(*config)
	requireMotion(e)
	var cfg struct {
		Recordings []struct{ ID, Tracks string } `json:"recordings"`
	}
	requireMotion(json.Unmarshal(b, &cfg))
	requireMotion(os.MkdirAll(*outDir, 0700))
	for _, r := range cfg.Recordings {
		if filepath.Base(r.ID) != r.ID {
			panic("invalid recording ID")
		}
		f, e := os.Open(r.Tracks)
		requireMotion(e)
		ts, e := tracks.Read(f)
		f.Close()
		requireMotion(e)
		groups := map[string][]int{}
		for i, t := range ts {
			groups[motionIdentity(t.ID)] = append(groups[motionIdentity(t.ID)], i)
		}
		ids := []string{}
		for id := range groups {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		sourceFiles := []any{}
		reports := []any{}
		for _, id := range ids {
			if len(id) != 6 {
				continue
			}
			lo, hi := math.Inf(1), math.Inf(-1)
			for _, i := range groups[id] {
				lo = math.Min(lo, ts[i].Points[0].Time)
				hi = math.Max(hi, ts[i].Points[len(ts[i].Points)-1].Time)
			}
			day := time.Unix(int64(lo), 0).UTC()
			day = time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, time.UTC)
			var source []tracks.Track
			for ; float64(day.Unix()) <= hi; day = day.AddDate(0, 0, 1) {
				path := filepath.Join(*cache, day.Format("2006-01-02"), id+".json")
				if _, e := os.Stat(path); os.IsNotExist(e) {
					continue
				}
				var buf bytes.Buffer
				_, e := tracks.Import(path, &buf, tracks.ImportOptions{Start: lo - .01, End: hi + .01, MaxGap: 30})
				if e != nil {
					sourceFiles = append(sourceFiles, map[string]any{"path": path, "sha256": motionSHA(path), "error": e.Error()})
					continue
				}
				read, e := tracks.Read(&buf)
				if e != nil {
					sourceFiles = append(sourceFiles, map[string]any{"path": path, "sha256": motionSHA(path), "error": e.Error()})
					continue
				}
				source = append(source, read...)
				sourceFiles = append(sourceFiles, map[string]any{"path": path, "sha256": motionSHA(path)})
			}
			for _, i := range groups[id] {
				enriched, stats := attachMotion(ts[i], source)
				ts[i] = enriched
				reports = append(reports, map[string]any{"track_id": ts[i].ID, "counts": stats})
			}
		}
		dir := filepath.Join(*outDir, r.ID)
		requireMotion(os.MkdirAll(dir, 0700))
		f, e = os.Create(filepath.Join(dir, "tracks.csv"))
		requireMotion(e)
		requireMotion(tracks.Write(f, ts))
		requireMotion(f.Close())
		motionSave(filepath.Join(dir, "provenance.json"), map[string]any{"input_tracks": r.Tracks, "input_sha256": motionSHA(r.Tracks), "output_sha256": motionSHA(filepath.Join(dir, "tracks.csv")), "recording": r.ID, "sources": sourceFiles, "tracks": reports, "note": "Only measured motion is attached. Original track IDs, timestamps, coordinates, heights and aircraft types are retained. Source absolute times must agree within 5 ms and positions within 1 m; unmatched values stay missing. Receiver/audio/GPS truth is not used. Barometric climb rate is never substituted for missing geometric climb rate.", "created_utc": time.Now().UTC()})
		fmt.Printf("%s: %d original tracks enriched from %d cached source files\n", r.ID, len(ts), len(sourceFiles))
	}
}
