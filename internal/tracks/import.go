package tracks

import (
	"bufio"
	"compress/gzip"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"pinpoint/internal/geo"
)

type ImportOptions struct {
	Start, End, MaxGap float64
	Bounds             *geo.Bounds
}
type ImportStats struct{ Files, Points, MissingGeometric, Stale, Ground, Filtered, DuplicateUpdates, ConflictingUpdates int }
type traceFile struct {
	ICAO      string              `json:"icao"`
	TypeCode  string              `json:"t"`
	Timestamp float64             `json:"timestamp"`
	Trace     [][]json.RawMessage `json:"trace"`
}

func number(r json.RawMessage) (float64, bool) {
	// The enclosing JSON decoder already validated the token syntax. Parsing
	// numeric fields directly avoids millions of tiny decoder allocations.
	v, err := strconv.ParseFloat(strings.TrimSpace(string(r)), 64)
	if err != nil || !geo.Finite(v) {
		return 0, false
	}
	return v, true
}

// Import reads extracted readsb traces recursively, one aircraft at a time.
// Only geometric altitude is accepted. Pressure altitude is not a substitute
// for WGS84 ellipsoidal height. Every invalid/filtered point breaks continuity.
func Import(path string, w io.Writer, o ImportOptions) (ImportStats, error) {
	return ImportContext(context.Background(), path, w, o)
}

func ImportContext(ctx context.Context, path string, w io.Writer, o ImportOptions) (ImportStats, error) {
	var stats ImportStats
	cw := csv.NewWriter(w)
	if err := cw.Write(KinematicHeader); err != nil {
		return stats, err
	}
	err := filepath.WalkDir(path, func(path string, d fs.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".json") && !strings.HasSuffix(path, ".json.gz") {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		return importTrace(f, path, cw, o, &stats)
	})
	cw.Flush()
	if err != nil {
		return stats, err
	}
	if err = cw.Error(); err != nil {
		return stats, err
	}
	if stats.Points == 0 {
		return stats, fmt.Errorf("no usable geometric-altitude points; check time/region filters and archive coverage")
	}
	return stats, nil
}

func importTrace(r io.Reader, path string, cw *csv.Writer, o ImportOptions, stats *ImportStats) error {
	// readsb archives commonly gzip trace_full_*.json without a .gz suffix.
	// Detect the stream format instead of relying on its filename.
	br := bufio.NewReader(r)
	r = br
	magic, _ := br.Peek(2)
	if len(magic) == 2 && magic[0] == 0x1f && magic[1] == 0x8b {
		g, err := gzip.NewReader(br)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		defer g.Close()
		r = g
	}
	var tr traceFile
	if err := json.NewDecoder(io.LimitReader(r, 128<<20)).Decode(&tr); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if len(tr.Trace) == 0 {
		return nil
	}
	if tr.ICAO == "" || !geo.Finite(tr.Timestamp) {
		return fmt.Errorf("invalid trace header: %s", path)
	}
	stats.Files++
	leg := 0
	last := -1e100
	for index := 0; index < len(tr.Trace); index++ {
		row := tr.Trace[index]
		if len(row) < 8 {
			return fmt.Errorf("short trace row in %s", path)
		}
		dt, ok := number(row[0])
		if !ok {
			return fmt.Errorf("invalid trace timestamp in %s", path)
		}
		ts := tr.Timestamp + dt
		conflict := false
		firstRow := row
		for index+1 < len(tr.Trace) {
			next := tr.Trace[index+1]
			if len(next) < 8 {
				break
			}
			nextTime, ok := number(next[0])
			if !ok || nextTime != dt {
				break
			}
			// Timestamps have only centisecond precision; repeated position
			// updates within ordinary quantization/GNSS noise are common.
			a, aok := tracePosition(firstRow)
			b, bok := tracePosition(next)
			if !aok || !bok || a.Sub(b).Norm() > 10 {
				conflict = true
			}
			stats.DuplicateUpdates++
			row = next
			index++
		}
		if conflict {
			stats.ConflictingUpdates++
			leg++
			last = ts
			continue
		}
		if ts < o.Start || ts > o.End {
			stats.Filtered++
			leg++
			last = ts
			continue
		}
		flags, fok := number(row[6])
		if !fok {
			return fmt.Errorf("invalid flags in %s", path)
		}
		if int(flags)&2 != 0 || ts <= last || ts-last > o.MaxGap {
			leg++
		}
		last = ts
		if int(flags)&1 != 0 {
			stats.Stale++
			leg++
			continue
		}
		if string(row[3]) == `"ground"` {
			stats.Ground++
			leg++
			continue
		}
		lat, lok := number(row[1])
		lon, ook := number(row[2])
		if !lok || !ook || lat < -90 || lat > 90 || lon < -180 || lon > 180 {
			return fmt.Errorf("invalid coordinates in %s", path)
		}
		if o.Bounds != nil && !o.Bounds.Contains(lat, lon) {
			stats.Filtered++
			leg++
			continue
		}
		alt, aok := 0.0, false
		if len(row) > 10 {
			alt, aok = number(row[10])
		}
		if !aok && int(flags)&8 != 0 {
			alt, aok = number(row[3])
		}
		if !aok {
			stats.MissingGeometric++
			leg++
			continue
		}
		alt *= 0.3048
		if alt < -1000 || alt > 30000 {
			return fmt.Errorf("invalid geometric altitude in %s", path)
		}
		id := fmt.Sprintf("%s-%.3f-%d", tr.ICAO, tr.Timestamp, leg)
		rec := []string{id}
		for _, v := range []float64{ts, lat, lon, alt} {
			rec = append(rec, strconv.FormatFloat(v, 'f', 8, 64))
		}
		rec = append(rec, strings.ToUpper(strings.TrimSpace(tr.TypeCode)))
		rec = append(rec, traceKinematics(row, int(flags))...)
		if err := cw.Write(rec); err != nil {
			return err
		}
		stats.Points++
	}
	return nil
}

func tracePosition(row []json.RawMessage) (geo.Vec, bool) {
	lat, a := number(row[1])
	lon, b := number(row[2])
	flags, c := number(row[6])
	alt, d := 0.0, false
	if len(row) > 10 {
		alt, d = number(row[10])
	}
	if !d && int(flags)&8 != 0 {
		alt, d = number(row[3])
	}
	if !a || !b || !c || !d || lat < -90 || lat > 90 || lon < -180 || lon > 180 {
		return geo.Vec{}, false
	}
	return geo.ECEF(lat, lon, alt*0.3048), true
}
