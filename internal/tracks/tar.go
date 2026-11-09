package tracks

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"sync"
)

// ImportTarArchives reads each day's ordered split tar parts directly, without
// extracting a multi-GB archive or trusting archive paths on the filesystem.
// Independent traces are decoded concurrently with bounded memory. CSV row order
// may vary; Read sorts by track ID and timestamp before any location scoring.
func ImportTarArchives(ctx context.Context, archives [][]string, out io.Writer, options ImportOptions) (ImportStats, error) {
	var stats ImportStats
	cw := csv.NewWriter(out)
	if err := cw.Write(KinematicHeader); err != nil {
		return stats, err
	}
	cw.Flush()
	if err := cw.Error(); err != nil {
		return stats, err
	}
	for _, parts := range archives {
		if err := importTar(ctx, parts, out, options, &stats); err != nil {
			return stats, err
		}
	}
	if stats.Points == 0 {
		return stats, fmt.Errorf("no usable geometric-altitude points in the selected time and area")
	}
	return stats, nil
}
func importTar(ctx context.Context, parts []string, out io.Writer, o ImportOptions, stats *ImportStats) error {
	var readers []io.Reader
	for _, path := range parts {
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		readers = append(readers, f)
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error
	fail := func(err error) {
		mu.Lock()
		if firstErr == nil {
			firstErr = err
			cancel()
		}
		mu.Unlock()
	}
	slots := make(chan struct{}, min(8, runtime.GOMAXPROCS(0)))
	tr := tar.NewReader(io.MultiReader(readers...))
loop:
	for {
		if ctx.Err() != nil {
			break
		}
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			fail(err)
			break
		}
		if h.Typeflag != tar.TypeReg && h.Typeflag != tar.TypeRegA {
			continue
		}
		if !strings.HasSuffix(h.Name, ".json") && !strings.HasSuffix(h.Name, ".json.gz") {
			continue
		}
		if h.Size > 128<<20 {
			fail(fmt.Errorf("trace member exceeds 128 MiB: %s", h.Name))
			break
		}
		// Acquire before reading so only this many compressed and decoded traces
		// can be retained at once, even when one aircraft has an unusually long day.
		select {
		case slots <- struct{}{}:
		case <-ctx.Done():
			break loop
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			<-slots
			fail(err)
			break
		}
		wg.Add(1)
		go func(name string, data []byte) {
			defer wg.Done()
			defer func() { <-slots }()
			if ctx.Err() != nil {
				return
			}
			var buf bytes.Buffer
			cw := csv.NewWriter(&buf)
			var local ImportStats
			err := importTrace(bytes.NewReader(data), name, cw, o, &local)
			cw.Flush()
			if err == nil {
				err = cw.Error()
			}
			if err != nil {
				fail(err)
				return
			}
			mu.Lock()
			defer mu.Unlock()
			if firstErr != nil {
				return
			}
			n, err := out.Write(buf.Bytes())
			if err == nil && n != buf.Len() {
				err = io.ErrShortWrite
			}
			if err != nil {
				firstErr = err
				cancel()
				return
			}
			stats.Files += local.Files
			stats.Points += local.Points
			stats.MissingGeometric += local.MissingGeometric
			stats.Stale += local.Stale
			stats.Ground += local.Ground
			stats.Filtered += local.Filtered
			stats.DuplicateUpdates += local.DuplicateUpdates
			stats.ConflictingUpdates += local.ConflictingUpdates
		}(h.Name, data)
	}
	wg.Wait()
	if firstErr != nil {
		return firstErr
	}
	return ctx.Err()
}
