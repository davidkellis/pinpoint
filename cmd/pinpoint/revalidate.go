package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"pinpoint/internal/acoustic"
	"pinpoint/internal/search"
	"pinpoint/internal/tracks"
)

func revalidateSequences(ctx context.Context, fs *flag.FlagSet, args []string) error {
	report := fs.String("report", "", "original completed automatic.json; preserved unchanged")
	binary := fs.String("training-binary", "", "original search executable (hashed, never executed)")
	audio := fs.String("audio", "", "original recording, verified against its recorded digest")
	out := fs.String("out", "", "new directory for revalidated reports")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *report == "" || *binary == "" || *audio == "" || *out == "" || fs.NArg() != 0 {
		return fmt.Errorf("supply --report, --training-binary, --audio and a new --out directory")
	}
	dir, err := filepath.Abs(*out)
	if err != nil {
		return err
	}
	if _, err = os.Stat(dir); !os.IsNotExist(err) {
		return fmt.Errorf("--out must be a new directory to preserve the original results")
	}
	data, err := os.ReadFile(*report)
	if err != nil {
		return err
	}
	var saved search.SequenceReport
	if err = json.Unmarshal(data, &saved); err != nil || saved.Settings == nil {
		return fmt.Errorf("invalid sequence report")
	}
	sp, err := acoustic.Load(saved.Settings.Spectrum)
	if err != nil {
		return err
	}
	f, err := os.Open(saved.Settings.Tracks)
	if err != nil {
		return err
	}
	ts, err := tracks.Read(f)
	f.Close()
	if err != nil {
		return err
	}
	r, err := search.RevalidateSequences(ctx, data, *binary, *audio, filepath.Join(dir, "spectra-cache"), sp, ts)
	if err != nil {
		return err
	}
	original, err := filepath.Abs(*report)
	if err != nil {
		return err
	}
	if err = search.WriteSequenceReport(dir, r, func(id int) string {
		path := filepath.Join(filepath.Dir(original), fmt.Sprintf("group-%03d", id), "map.html")
		rel, _ := filepath.Rel(dir, path)
		return filepath.ToSlash(rel)
	}); err != nil {
		return err
	}
	fmt.Printf("%s; training winners unchanged; review %s\n", r.Status, filepath.Join(dir, "automatic.html"))
	return nil
}
