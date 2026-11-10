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
)

// sounds explains, in plain language, what the detector found in a recording
// and which sounds can help locate it.
func sounds(ctx context.Context, fs *flag.FlagSet, args []string) error {
	audio := fs.String("audio", "", "recording (used for loudness, and analyzed afresh unless --detection is given)")
	detectionPath := fs.String("detection", "", "existing detection.json to explain instead of analyzing --audio")
	profile := fs.String("profile", acoustic.HybridProfile, "acoustic profile when analyzing: standard, short-pass or hybrid")
	maxHz := fs.Float64("max-hz", acoustic.DefaultLocatableMaxHz, "highest median pitch offered to location search")
	minCurves := fs.Int("min-curves", 2, "curves the consensus search requires")
	out := fs.String("out", "", "optional directory for sounds.json (and detection.json when analyzing)")
	asJSON := fs.Bool("json", false, "print JSON instead of text")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *audio == "" && *detectionPath == "" || fs.NArg() != 0 {
		return fmt.Errorf("supply --audio, --detection, or both")
	}
	var report acoustic.DetectionReport
	if *detectionPath != "" {
		b, err := os.ReadFile(*detectionPath)
		if err != nil {
			return err
		}
		if err = json.Unmarshal(b, &report); err != nil {
			return err
		}
	} else {
		var err error
		switch *profile {
		case acoustic.HybridProfile:
			_, report, err = acoustic.AnalyzeHybrid(ctx, *audio)
		case acoustic.ShortPassProfile:
			_, report, err = acoustic.AnalyzeShortPass(ctx, *audio, acoustic.ShortPassOptions())
		case "standard":
			_, report, err = acoustic.AnalyzeMultiResolution(ctx, *audio)
		default:
			return fmt.Errorf("unknown detector profile")
		}
		if err != nil {
			return err
		}
	}
	var humps []acoustic.Hump
	if *audio != "" {
		l, err := acoustic.LoudnessEnvelope(ctx, *audio, 0, 50, 2000, 1)
		if err != nil {
			return err
		}
		humps = acoustic.FindHumps(l, acoustic.DefaultHumpOptions())
	}
	inv := acoustic.Inventory(report.Events, humps, *maxHz, *minCurves)
	if *out != "" {
		if err := os.MkdirAll(*out, 0755); err != nil {
			return err
		}
		if *detectionPath == "" {
			if err := search.JSON(filepath.Join(*out, "detection.json"), report); err != nil {
				return err
			}
		}
		if err := search.JSON(filepath.Join(*out, "sounds.json"), inv); err != nil {
			return err
		}
	}
	if *asJSON {
		e := json.NewEncoder(os.Stdout)
		e.SetIndent("", "  ")
		return e.Encode(inv)
	}
	fmt.Print(inv.Text())
	return nil
}
