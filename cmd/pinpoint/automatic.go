package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"pinpoint/internal/acoustic"
	"pinpoint/internal/search"
	"pinpoint/internal/tracks"
)

func automaticLocate(ctx context.Context, fs *flag.FlagSet, args []string) error {
	config := fs.String("config", "", "region, start time, flight history and terrain; event windows are ignored")
	audio := fs.String("audio", "", "recording to analyze afresh")
	detection := fs.String("detection", "", "reuse a detection report with the configuration's original spectrum, instead of --audio")
	profile := fs.String("profile", "standard", "standard, short-pass, or hybrid acoustic analysis")
	method := fs.String("method", "sequences", "sequences with reserved audio, or legacy pairs")
	validationAudio := fs.String("validation-audio", "", "original audio for reserved-block validation when reusing --detection")
	maxGroups := fs.Int("max-groups", 2048, "explicit per-trial sequence-group budget")
	out := fs.String("out", "work/automatic", "automatic group reports and maps")
	controls := fs.Int("controls", 8, "wrong-time searches: 0 for unchecked diagnostics, 2, 4, or 8")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *config == "" || (*audio == "") == (*detection == "") || fs.NArg() != 0 || (*controls != 0 && *controls != 2 && *controls != 4 && *controls != 8) || (*method != "sequences" && *method != "pairs") {
		return fmt.Errorf("supply --config and exactly one of --audio or --detection; --method sequences|pairs; --controls 0, 2, 4 or 8")
	}
	path, err := filepath.Abs(*config)
	if err != nil {
		return err
	}
	c, err := readConfig(path)
	if err != nil {
		return err
	}
	dir, err := filepath.Abs(*out)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	var s *acoustic.Spectrum
	var d acoustic.DetectionReport
	if *audio != "" {
		switch *profile {
		case "standard":
			s, d, err = acoustic.AnalyzeMultiResolution(ctx, *audio)
		case acoustic.ShortPassProfile:
			s, d, err = acoustic.AnalyzeShortPass(ctx, *audio, acoustic.ShortPassOptions())
		case acoustic.HybridProfile:
			s, d, err = acoustic.AnalyzeHybrid(ctx, *audio)
		default:
			return fmt.Errorf("unknown detector profile")
		}
		if err != nil {
			return err
		}
		c.Spectrum = filepath.Join(dir, "spectrogram.bin")
		if err = s.Save(c.Spectrum); err != nil {
			return err
		}
	} else {
		s, err = acoustic.Load(c.Spectrum)
		if err != nil {
			return err
		}
		b, e := os.ReadFile(*detection)
		if e != nil {
			return e
		}
		if err = json.Unmarshal(b, &d); err != nil {
			return err
		}
		if d.Version != acoustic.DetectorVersion && d.Version != acoustic.ShortPassVersion && d.Version != acoustic.HybridVersion {
			return fmt.Errorf("cached detections use another version; reanalyze the audio")
		}
	}
	if err = search.JSON(filepath.Join(dir, "detection.json"), d); err != nil {
		return err
	}
	c.Events = acoustic.SearchEvents(d.Events, true, false)
	if len(c.Events) < 2 {
		return fmt.Errorf("found %d strong curves usable for joint timing; insufficient evidence", len(c.Events))
	}
	c, err = search.MeasuredJointConfig(c, c.Events)
	if err != nil {
		return err
	}
	c.CurveEnvelope = true
	if err = search.JSON(filepath.Join(dir, "automatic-search.json"), c); err != nil {
		return err
	}
	f, err := os.Open(c.Tracks)
	if err != nil {
		return err
	}
	ts, err := tracks.Read(f)
	f.Close()
	if err != nil {
		return err
	}
	if *method == "sequences" {
		o := search.DefaultSequenceOptions(c)
		o.MaxGroups = *maxGroups
		o.CacheDir = filepath.Join(dir, "sequence-cache")
		o.AudioPath = *validationAudio
		if o.AudioPath == "" {
			o.AudioPath = *audio
		}
		if o.AudioPath == "" {
			o.AudioPath = s.Source
		}
		o.ControlShifts = o.ControlShifts[:*controls]
		last := time.Time{}
		r, runErr := search.RunAutomaticSequences(ctx, c, s, ts, o, func(p search.SequenceProgress) {
			if p.Stage == "starting" || p.Stage == "finished" || time.Since(last) > 10*time.Second {
				if p.Trials == 0 {
					fmt.Fprintf(os.Stderr, "Preparing training and reserved sound: %d/%d curves\n", p.Done, p.Total)
				} else {
					fmt.Fprintf(os.Stderr, "Trial %d/%d (%+.0fs), %d-event group %d/%d: %s %d/%d\n", p.Trial, p.Trials, p.Shift, p.Size, p.Group, p.Groups, p.Stage, p.Done, p.Total)
				}
				last = time.Now()
			}
		}, func(g search.SequenceGroup, q search.Config, r search.Report) error {
			if g.Trial != 0 || g.Best == nil {
				return nil
			}
			path := filepath.Join(dir, fmt.Sprintf("group-%03d", g.ID))
			r.Note += " This map is fitted on training intervals; inspect automatic.html for frozen reserved-audio checks and complete wrong-time controls."
			if err := search.WriteReport(path, r, q, s, ts); err != nil {
				return err
			}
			if err := search.JSON(filepath.Join(path, "validation.json"), g); err != nil {
				return err
			}
			return search.JSON(filepath.Join(path, "search.json"), q)
		})
		writeErr := search.WriteSequenceReport(dir, r, func(id int) string { return fmt.Sprintf("group-%03d/map.html", id) })
		if runErr != nil {
			return runErr
		}
		if writeErr != nil {
			return writeErr
		}
		fmt.Printf("%s; review %s\n", r.Status, filepath.Join(dir, "automatic.html"))
		if r.Status == "incomplete" {
			return fmt.Errorf("some sequence searches were incomplete; inspect automatic.json")
		}
		return nil
	}
	o := search.DefaultAutomaticOptions(c)
	o.CacheDir = filepath.Join(dir, "cache")
	if *controls == 0 {
		o.ControlShifts = nil
	} else if *controls == 4 {
		o.ControlShifts = append(o.ControlShifts, 2*o.ControlShifts[0], 2*o.ControlShifts[1])
	} else if *controls == 8 {
		o.ControlShifts = search.DefaultSequenceOptions(c).ControlShifts
	}
	last := time.Time{}
	r, err := search.RunAutomatic(ctx, c, s, ts, o, func(p search.GroupProgress) {
		if p.Stage == "starting" || p.Stage == "finished" || time.Since(last) > 10*time.Second {
			fmt.Fprintf(os.Stderr, "Trial %d/%d (%+.0fs), pair %d/%d: %s %d/%d\n", p.Trial, p.Trials, p.Shift, p.Pair, p.Pairs, p.Stage, p.Done, p.Total)
			last = time.Now()
		}
	}, func(g search.GroupResult, c search.Config, r search.Report) error {
		if g.Trial != 0 || g.Best == nil {
			return nil
		}
		path := filepath.Join(dir, fmt.Sprintf("pair-%03d", g.Pair))
		if err := search.WriteReport(path, r, c, s, ts); err != nil {
			return err
		}
		return search.JSON(filepath.Join(path, "search.json"), c)
	})
	writeErr := search.WriteAutomaticReport(dir, r, func(pair int) string { return fmt.Sprintf("pair-%03d/map.html", pair) })
	if err != nil {
		return err
	}
	if writeErr != nil {
		return writeErr
	}
	fmt.Printf("%s; review %s\n", r.Status, filepath.Join(dir, "automatic.html"))
	if r.Status == "incomplete" {
		return fmt.Errorf("some searches were incomplete; inspect automatic.json")
	}
	return nil
}
