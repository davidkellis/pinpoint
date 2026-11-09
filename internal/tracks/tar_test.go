package tracks

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestGzipTraceWithoutGzipFilename(t *testing.T) {
	const raw = `{"icao":"abc123","timestamp":1000,"trace":[[0,41,-74,5000,140,90,0,0,null,"adsb_icao",5200],[5,41,-73.995,5000,140,90,0,0,null,"adsb_icao",5200]]}`
	var compressed bytes.Buffer
	g := gzip.NewWriter(&compressed)
	g.Write([]byte(raw))
	if err := g.Close(); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "trace_full_abc123.json")
	if err := os.WriteFile(path, compressed.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	o := ImportOptions{Start: 1000, End: 1010, MaxGap: 10}
	var extracted bytes.Buffer
	stats, err := Import(path, &extracted, o)
	if err != nil || stats.Points != 2 {
		t.Fatalf("compressed .json import failed: %+v %v", stats, err)
	}
	var archive bytes.Buffer
	tw := tar.NewWriter(&archive)
	tw.WriteHeader(&tar.Header{Name: "traces/23/trace_full_abc123.json", Mode: 0600, Size: int64(compressed.Len())})
	tw.Write(compressed.Bytes())
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	// Split in the compressed member, exercising the real archive layout.
	cut := 512 + compressed.Len()/2
	a, b := filepath.Join(dir, "day.tar.aa"), filepath.Join(dir, "day.tar.ab")
	os.WriteFile(a, archive.Bytes()[:cut], 0600)
	os.WriteFile(b, archive.Bytes()[cut:], 0600)
	var direct bytes.Buffer
	stats, err = ImportTarArchives(context.Background(), [][]string{{a, b}}, &direct, o)
	if err != nil || stats.Points != 2 || direct.String() != extracted.String() {
		t.Fatalf("split tar import changed trace: %+v %v", stats, err)
	}
	parsed, err := Read(&direct)
	if err != nil || len(parsed) != 1 || len(parsed[0].Points) != 2 {
		t.Fatalf("imported data cannot be searched: %v", err)
	}
}

func TestConcurrentArchiveImportKeepsAllTracks(t *testing.T) {
	var archive bytes.Buffer
	tw := tar.NewWriter(&archive)
	for i := 0; i < 32; i++ {
		raw := []byte(fmt.Sprintf(`{"icao":"%06x","timestamp":1000,"trace":[[0,41,-74,5000,140,90,0,0,null,"adsb_icao",5200],[5,41,-73.995,5000,140,90,0,0,null,"adsb_icao",5200]]}`, i))
		if i%2 == 0 {
			var compressed bytes.Buffer
			g := gzip.NewWriter(&compressed)
			g.Write(raw)
			g.Close()
			raw = compressed.Bytes()
		}
		tw.WriteHeader(&tar.Header{Name: fmt.Sprintf("trace_full_%06x.json", i), Mode: 0600, Size: int64(len(raw))})
		tw.Write(raw)
	}
	tw.Close()
	p := filepath.Join(t.TempDir(), "day.tar")
	os.WriteFile(p, archive.Bytes(), 0600)
	var out bytes.Buffer
	stats, err := ImportTarArchives(context.Background(), [][]string{{p}}, &out, ImportOptions{Start: 1000, End: 1010, MaxGap: 10})
	if err != nil || stats.Files != 32 || stats.Points != 64 {
		t.Fatalf("lost concurrent results: %+v %v", stats, err)
	}
	ts, err := Read(&out)
	if err != nil || len(ts) != 32 {
		t.Fatalf("interleaved CSV writes: %v", err)
	}
	for _, track := range ts {
		if len(track.Points) != 2 || track.Points[1].Time != 1005 {
			t.Fatal("corrupted trace")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ImportTarArchives(ctx, [][]string{{p}}, &bytes.Buffer{}, ImportOptions{}); err != context.Canceled {
		t.Fatalf("cancellation was not propagated: %v", err)
	}
}
