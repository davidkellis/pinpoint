package tracks

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/binary"
	"encoding/csv"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"pinpoint/internal/geo"
)

// Replay uses the public tar1090 half-hour replay indexes only to discover
// aircraft IDs. Their pressure altitudes are NEVER used for acoustic geometry.
// Full readsb traces supply the required geometric altitudes.
// Format reference: wiedehopf/tar1090 html/script.js, initReplay/replayStep.
type ReplayStats struct {
	Aircraft   int         `json:"aircraft_discovered"`
	Downloaded int         `json:"traces_available"`
	Missing    []string    `json:"missing_traces"`
	Import     ImportStats `json:"import"`
}

type HistoryUnavailable struct{ Message string }

func (e *HistoryUnavailable) Error() string { return e.Message }

func Replay(ctx context.Context, cache string, w io.Writer, o ImportOptions, maxBytes int64, progress func(string, int, int)) (ReplayStats, error) {
	var stats ReplayStats
	if o.Bounds == nil || !o.Bounds.Valid() || o.End <= o.Start || o.End-o.Start > 30*3600 {
		return stats, fmt.Errorf("replay requires bounds and a time window of at most 30 hours")
	}
	keys := map[string]bool{}
	first := time.Unix(int64(o.Start), 0).UTC().Truncate(30 * time.Minute)
	remaining := maxBytes
	for t := first; float64(t.Unix()) <= o.End; t = t.Add(30 * time.Minute) {
		index := t.Hour()*2 + t.Minute()/30
		date := t.Format("2006/01/02")
		path := filepath.Join(cache, "replay", t.Format("2006-01-02"), fmt.Sprintf("%02d.bin", index))
		url := fmt.Sprintf("https://globe.adsb.lol/globe_history/%s/heatmap/%02d.bin.ttf", date, index)
		if progress != nil {
			progress("Reading flight replay for "+t.Format("Jan 2 15:04 UTC"), 0, 0)
		}
		b, e := historyFile(ctx, path, url, &remaining, false)
		if e != nil {
			return stats, e
		}
		ids, e := replayIDs(b, *o.Bounds, o.Start, o.End)
		if e != nil {
			return stats, e
		}
		for id := range ids {
			keys[t.Format("2006-01-02")+"/"+id] = true
		}
	}
	ordered := make([]string, 0, len(keys))
	for k := range keys {
		ordered = append(ordered, k)
	}
	sort.Strings(ordered)
	stats.Aircraft = len(ordered)
	paths := make([]string, len(ordered))
	var mu sync.Mutex
	done := 0
	var fatal error
	// Four requests at a time, with cache hits avoiding network requests.
	jobs := make(chan int)
	var wg sync.WaitGroup
	for worker := 0; worker < 4; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				key := ordered[i]
				day, id := key[:10], key[11:]
				date, _ := time.Parse("2006-01-02", day)
				current := time.Now().UTC().Before(date.Add(25 * time.Hour))
				url := "https://globe.adsb.lol/globe_history/" + date.Format("2006/01/02") + "/traces/" + id[len(id)-2:] + "/trace_full_" + id + ".json"
				if current {
					url = "https://globe.adsb.lol/data/traces/" + id[len(id)-2:] + "/trace_full_" + id + ".json"
				}
				path := filepath.Join(cache, "traces", day, id+".json")
				refresh := current
				// A trace fetched after this closed search window can be reused
				// even while the aircraft continues flying later in the day.
				if st, e := os.Stat(path); e == nil && st.ModTime().After(time.Unix(int64(o.End)+300, 0)) {
					refresh = false
				}
				// Reserve a bounded per-file budget before concurrent requests.
				mu.Lock()
				budget := min(16<<20, remaining)
				remaining -= budget
				mu.Unlock()
				before := budget
				_, e := historyFile(ctx, path, url, &budget, refresh)
				mu.Lock()
				remaining += budget
				done++
				if e != nil {
					stats.Missing = append(stats.Missing, key)
					if before == 0 {
						fatal = fmt.Errorf("flight history exceeds download limit")
					}
				} else {
					paths[i] = path
					stats.Downloaded++
				}
				if progress != nil {
					progress("Downloading geometric flight traces", done, len(ordered))
				}
				mu.Unlock()
			}
		}()
	}
	for i := range ordered {
		select {
		case jobs <- i:
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return stats, ctx.Err()
		}
	}
	close(jobs)
	wg.Wait()
	if fatal != nil {
		return stats, fatal
	}
	if err := ctx.Err(); err != nil {
		return stats, err
	}
	sort.Strings(stats.Missing)
	cw := csv.NewWriter(w)
	if e := cw.Write(KinematicHeader); e != nil {
		return stats, e
	}
	for _, path := range paths {
		if path == "" {
			continue
		}
		f, e := os.Open(path)
		if e != nil {
			return stats, e
		}
		e = importTrace(f, path, cw, o, &stats.Import)
		f.Close()
		if e != nil {
			return stats, e
		}
	}
	cw.Flush()
	if e := cw.Error(); e != nil {
		return stats, e
	}
	if stats.Downloaded == 0 && len(keys) > 0 {
		return stats, &HistoryUnavailable{"Flight replay is available, but geometric traces are not yet available. Retry this search later; audio analysis is saved."}
	}
	return stats, nil
}

func replayIDs(raw []byte, b geo.Bounds, start, end float64) (map[string]bool, error) {
	if len(raw)%16 != 0 {
		return nil, fmt.Errorf("invalid flight replay length")
	}
	ids := map[string]bool{}
	stamp := 0.0
	header := false
	for p := 0; p < len(raw); p += 16 {
		u := func(n int) uint32 { return binary.LittleEndian.Uint32(raw[p+n : p+n+4]) }
		id := u(0)
		if id == 0x0e7f7c9d {
			stamp = float64(u(4))*4294967.296 + float64(u(8))/1000
			header = true
			continue
		}
		if !header {
			// Replay files can begin with a slice-offset table. tar1090 scans
			// for the first magic timestamp record before interpreting positions.
			continue
		}
		if stamp < start-60 || stamp > end+60 || id&0x1000000 != 0 {
			continue
		}
		lat, lon := float64(int32(u(4)))/1e6, float64(int32(u(8)))/1e6
		if b.Contains(lat, lon) {
			ids[fmt.Sprintf("%06x", id&0xffffff)] = true
		}
	}
	if !header {
		return nil, fmt.Errorf("flight replay contains no timestamps")
	}
	return ids, nil
}

func historyFile(ctx context.Context, path, url string, remaining *int64, refresh bool) ([]byte, error) {
	if st, e := os.Stat(path); e == nil && (!refresh || time.Since(st.ModTime()) < 15*time.Minute) {
		return os.ReadFile(path)
	}
	req, e := http.NewRequestWithContext(ctx, "GET", url, nil)
	if e != nil {
		return nil, e
	}
	client := &http.Client{Timeout: 90 * time.Second}
	r, e := client.Do(req)
	if e != nil {
		return nil, e
	}
	defer r.Body.Close()
	if r.StatusCode == 404 {
		return nil, &HistoryUnavailable{"Flight history is not published for this time yet. Retry later; the audio analysis is saved."}
	}
	if r.StatusCode != 200 {
		return nil, fmt.Errorf("flight history HTTP %d", r.StatusCode)
	}
	limit := min(*remaining, 128<<20)
	raw, e := io.ReadAll(io.LimitReader(r.Body, limit+1))
	*remaining -= min(int64(len(raw)), limit)
	if e != nil {
		return nil, e
	}
	if int64(len(raw)) > limit {
		return nil, fmt.Errorf("flight history exceeds download limit")
	}
	b := raw
	if len(raw) > 2 && raw[0] == 0x1f && raw[1] == 0x8b {
		g, e := gzip.NewReader(bytes.NewReader(raw))
		if e != nil {
			return nil, e
		}
		b, e = io.ReadAll(io.LimitReader(g, 512<<20+1))
		g.Close()
		if e != nil {
			return nil, e
		}
		if len(b) > 512<<20 {
			return nil, fmt.Errorf("flight replay decompression exceeds limit")
		}
	}
	if e = os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return nil, e
	}
	f, e := os.CreateTemp(filepath.Dir(path), ".history-*")
	if e != nil {
		return nil, e
	}
	defer os.Remove(f.Name())
	_, e = f.Write(b)
	ce := f.Close()
	if e != nil {
		return nil, e
	}
	if ce != nil {
		return nil, ce
	}
	if e = os.Rename(f.Name(), path); e != nil {
		return nil, e
	}
	return b, nil
}
