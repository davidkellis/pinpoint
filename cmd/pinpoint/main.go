package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"pinpoint/internal/acoustic"
	"pinpoint/internal/demo"
	"pinpoint/internal/geo"
	"pinpoint/internal/search"
	"pinpoint/internal/tracks"
	"pinpoint/internal/webapp"
)

const usage = `pinpoint — acoustic aircraft geolocation (research prototype)

  pinpoint demo    --out demo-output
  pinpoint analyze --audio recording.m4a --out work [--channel 0]
  pinpoint fetch   --date 2025-09-08 [--instance prod-0] [--download --out data/archive]
  pinpoint import  --input data/extracted --from RFC3339 --until RFC3339 --out work/tracks.csv
                   [--bounds south,west,north,east]
  pinpoint locate  --config search.json --out work/results
  pinpoint joint-locate --audio recording.wav --config reviewed-windows.json --out work/measured-joint
  pinpoint auto-locate --audio recording.wav --config region.json --out work/automatic
  pinpoint consensus --detection detection.json --tracks tracks.csv --start RFC3339 --state TX --out work/consensus
  pinpoint revalidate-sequences --report OLD/automatic.json --training-binary ORIGINAL --audio recording.wav --out NEW
  pinpoint diagnose --config search.json --lat 37.66 --lon -121.78 --track ID --out work/fit
  pinpoint corroborate --config search.json --results results/results.json --out work/followup
  pinpoint map     --results work/results [--open]
  pinpoint serve   [--addr 127.0.0.1:8080] [--data-dir work/web]

Use a command with --help for its options. FFmpeg is required for audio decoding.
Audio provides Doppler curves; downloaded ADS-B provides aircraft trajectories.
`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := run(ctx, os.Args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		fmt.Fprintln(os.Stderr, "pinpoint:", err)
		os.Exit(1)
	}
}
func run(ctx context.Context, args []string) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		fmt.Print(usage)
		return nil
	}
	fs := flag.NewFlagSet(args[0], flag.ContinueOnError)
	fs.Usage = func() { printUsage(fs) }
	switch args[0] {
	case "serve":
		addr := fs.String("addr", "127.0.0.1:8080", "local web address")
		dir := fs.String("data-dir", "work/web", "uploaded recordings, jobs and flight archive cache")
		tracksFile := fs.String("tracks", "", "optional normalized flight CSV available to the web app")
		traceDir := fs.String("trace-dir", "", "optional extracted readsb archive directory available to the web app")
		maxUpload := fs.Int64("max-upload-mb", 512, "maximum upload size in MiB")
		maxDownload := fs.Float64("max-download-gb", 8, "maximum flight-data download per search in decimal GB")
		workers := fs.Int("workers", 0, "shared location-scoring worker limit (0: available Go CPUs, up to 32)")
		eventWorkers := fs.Int("event-workers", 2, "independent events processed concurrently (1–4; share the scoring worker limit)")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *maxUpload < 1 || *maxUpload > 4096 || !geo.Finite(*maxDownload) || *maxDownload <= 0 || *maxDownload > 100 {
			return fmt.Errorf("invalid --max-upload-mb or --max-download-gb limit")
		}
		return webapp.Listen(ctx, *addr, webapp.Options{DataDir: *dir, TracksFile: *tracksFile, TraceDir: *traceDir, MaxUploadBytes: *maxUpload << 20, MaxDownloadBytes: int64(*maxDownload * 1e9), Workers: *workers, EventWorkers: *eventWorkers})
	case "analyze":
		audio := fs.String("audio", "", "audio/video file decoded by FFmpeg")
		out := fs.String("out", "work", "output directory")
		o := acoustic.Defaults()
		profile := fs.String("detector-profile", "standard", "standard, short-pass, or combined hybrid analysis")
		fs.IntVar(&o.Rate, "sample-rate", o.Rate, "analysis sample rate; must exceed twice max-hz")
		fs.IntVar(&o.Channel, "channel", 0, "zero-based audio channel (avoid stereo cancellation)")
		fs.IntVar(&o.Window, "window", o.Window, "FFT samples, power of two")
		fs.IntVar(&o.Hop, "hop", o.Hop, "STFT hop in samples")
		fs.Float64Var(&o.MinHz, "min-hz", o.MinHz, "lowest analyzed frequency")
		fs.Float64Var(&o.MaxHz, "max-hz", o.MaxHz, "highest analyzed frequency")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *audio == "" {
			return fmt.Errorf("--audio is required")
		}
		_, err := analyze(ctx, *audio, *out, o, *profile)
		return err
	case "locate":
		config := fs.String("config", "", "search configuration JSON")
		out := fs.String("out", "work/results", "output directory")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *config == "" {
			return fmt.Errorf("--config is required")
		}
		c, err := readConfig(*config)
		if err != nil {
			return err
		}
		return locate(ctx, c, *out)
	case "joint-locate":
		return jointLocate(ctx, fs, args[1:])
	case "auto-locate":
		return automaticLocate(ctx, fs, args[1:])
	case "consensus":
		return consensusLocate(ctx, fs, args[1:])
	case "revalidate-sequences":
		return revalidateSequences(ctx, fs, args[1:])
	case "diagnose":
		config := fs.String("config", "", "search configuration JSON")
		lat := fs.Float64("lat", math.NaN(), "hypothesis latitude (not ground truth)")
		lon := fs.Float64("lon", math.NaN(), "hypothesis longitude")
		clock := fs.Float64("clock-offset", 0, "recording clock correction in seconds")
		id := fs.String("track", "", "exact trajectory ID to diagnose")
		out := fs.String("out", "work/fit-diagnostic", "output directory")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *config == "" || *id == "" {
			return fmt.Errorf("--config, --lat, --lon and --track are required")
		}
		c, err := readConfig(*config)
		if err != nil {
			return err
		}
		s, err := acoustic.Load(c.Spectrum)
		if err != nil {
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
		var selected []tracks.Track
		for _, t := range ts {
			if t.ID == *id {
				selected = append(selected, t)
			}
		}
		if len(selected) == 0 {
			return fmt.Errorf("track %q is not in the configured trajectories", *id)
		}
		r, err := search.Diagnose(c, s, selected, *lat, *lon, *clock)
		if err != nil {
			return err
		}
		if err = search.WriteDiagnostic(*out, r); err != nil {
			return err
		}
		for _, fit := range r.Fits {
			fmt.Printf("event %d: %s; exclusions %v\n", fit.Event, fit.Reason, fit.Rejections)
		}
		fmt.Println("Diagnostic:", filepath.Join(*out, "diagnostic.html"))
		return nil
	case "corroborate":
		return corroborate(ctx, fs, args[1:])
	case "map":
		results := fs.String("results", "work/demo/results", "directory containing a generated map.html")
		open := fs.Bool("open", false, "open the map in your default browser")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		path, err := filepath.Abs(filepath.Join(*results, "map.html"))
		if err != nil {
			return err
		}
		info, err := os.Stat(path)
		if err != nil {
			return fmt.Errorf("map unavailable; rerun locate with the recording's configuration to generate it: %w", err)
		}
		if info.IsDir() {
			return fmt.Errorf("expected a map.html file, got a directory")
		}
		link := (&url.URL{Scheme: "file", Path: filepath.ToSlash(path)}).String()
		fmt.Println(link)
		if *open {
			var cmd *exec.Cmd
			switch runtime.GOOS {
			case "darwin":
				cmd = exec.CommandContext(ctx, "open", link)
			case "windows":
				cmd = exec.CommandContext(ctx, "rundll32", "url.dll,FileProtocolHandler", link)
			default:
				cmd = exec.CommandContext(ctx, "xdg-open", link)
			}
			if err = cmd.Run(); err != nil {
				return fmt.Errorf("could not open browser; open %s manually: %w", path, err)
			}
		}
		return nil
	case "demo":
		out := fs.String("out", "demo-output", "new output directory")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if _, err := os.Stat(*out); !os.IsNotExist(err) {
			return fmt.Errorf("demo output must be a new directory: %s", *out)
		}
		fmt.Fprintln(os.Stderr, "Generating two synthetic flyovers, noise and four aircraft tracks...")
		if _, err := demo.Generate(*out); err != nil {
			return err
		}
		if _, err := analyze(ctx, filepath.Join(*out, "audio.wav"), *out, acoustic.Defaults(), "standard"); err != nil {
			return err
		}
		c, err := readConfig(filepath.Join(*out, "search.json"))
		if err != nil {
			return err
		}
		return locate(ctx, c, filepath.Join(*out, "results"))
	case "fetch":
		date := fs.String("date", "", "UTC date YYYY-MM-DD")
		instance := fs.String("instance", "prod-0", "adsb.lol archive instance")
		download := fs.Bool("download", false, "download listed parts (otherwise only show manifest)")
		out := fs.String("out", "data/archive", "download directory")
		limit := fs.Float64("max-gb", 8, "maximum total decimal GB to download")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if !geo.Finite(*limit) || *limit <= 0 || *limit > 100 {
			return fmt.Errorf("--max-gb must be in (0,100]")
		}
		networkCtx, cancel := context.WithTimeout(ctx, 2*time.Hour)
		defer cancel()
		assets, err := tracks.ListArchive(networkCtx, *date, *instance)
		if err != nil {
			return err
		}
		if err = json.NewEncoder(os.Stdout).Encode(assets); err != nil {
			return err
		}
		if !*download {
			return nil
		}
		fmt.Fprintln(os.Stderr, "Downloading archive parts...")
		return tracks.FetchArchive(networkCtx, assets, *out, int64(*limit*1e9))
	case "import":
		input := fs.String("input", "", "extracted readsb trace directory or JSON/JSON.gz file")
		archive := fs.String("archive", "", "glob matching all ordered split tar parts for one day (quote the glob)")
		from := fs.String("from", "", "earliest emission time, RFC3339")
		until := fs.String("until", "", "latest emission time, RFC3339")
		out := fs.String("out", "work/tracks.csv", "normalized output CSV")
		bounds := fs.String("bounds", "", "optional aircraft bounds south,west,north,east (pad beyond listener search area)")
		gap := fs.Float64("max-gap", 30, "maximum gap in seconds before splitting a track (archive cadence can be 20s)")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if (*input == "") == (*archive == "") {
			return fmt.Errorf("specify exactly one of --input or --archive")
		}
		a, err := parseTime(*from)
		if err != nil {
			return err
		}
		b, err := parseTime(*until)
		if err != nil {
			return err
		}
		if b <= a || !geo.Finite(*gap) || *gap <= 0 {
			return fmt.Errorf("invalid time range or gap")
		}
		o := tracks.ImportOptions{Start: a, End: b, MaxGap: *gap}
		if *bounds != "" {
			v := strings.Split(*bounds, ",")
			if len(v) != 4 {
				return fmt.Errorf("bounds must be south,west,north,east")
			}
			nums := make([]float64, 4)
			for i := range nums {
				nums[i], err = strconv.ParseFloat(v[i], 64)
				if err != nil {
					return err
				}
			}
			region := geo.Bounds{South: nums[0], West: nums[1], North: nums[2], East: nums[3]}
			if !region.Valid() {
				return fmt.Errorf("invalid bounds")
			}
			o.Bounds = &region
		}
		if err = os.MkdirAll(filepath.Dir(*out), 0755); err != nil {
			return err
		}
		f, err := os.CreateTemp(filepath.Dir(*out), ".import-*")
		if err != nil {
			return err
		}
		defer os.Remove(f.Name())
		var stats tracks.ImportStats
		if *archive != "" {
			parts, globErr := filepath.Glob(*archive)
			if globErr != nil || len(parts) == 0 {
				f.Close()
				return fmt.Errorf("--archive must match the split tar parts: %s", *archive)
			}
			stats, err = tracks.ImportTarArchives(ctx, [][]string{parts}, f, o)
		} else {
			stats, err = tracks.ImportContext(ctx, *input, f, o)
		}
		ce := f.Close()
		if err != nil {
			return fmt.Errorf("%w (stats: %+v)", err, stats)
		}
		if ce != nil {
			return ce
		}
		if err = os.Rename(f.Name(), *out); err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(stats)
	default:
		return fmt.Errorf("unknown command %q\n%s", args[0], usage)
	}
}
func printUsage(fs *flag.FlagSet) {
	fmt.Fprintf(fs.Output(), "Usage: pinpoint %s [options]\n\n", fs.Name())
	fs.VisitAll(func(f *flag.Flag) {
		valueName, description := flag.UnquoteUsage(f)
		fmt.Fprintf(fs.Output(), "  --%s", f.Name)
		if valueName != "" {
			fmt.Fprintf(fs.Output(), " %s", valueName)
		}
		fmt.Fprintf(fs.Output(), "\n      %s", description)
		if f.DefValue != "" {
			fmt.Fprintf(fs.Output(), " (default %s)", f.DefValue)
		}
		fmt.Fprintln(fs.Output())
	})
	fmt.Fprintln(fs.Output(), "  --help\n      Show command options")
}
func parseTime(s string) (float64, error) {
	t, e := time.Parse(time.RFC3339Nano, s)
	if e != nil {
		return 0, e
	}
	return float64(t.Unix()) + float64(t.Nanosecond())/1e9, nil
}
func readConfig(path string) (search.Config, error) {
	c := search.DefaultConfig()
	f, e := os.Open(path)
	if e != nil {
		return c, e
	}
	defer f.Close()
	d := json.NewDecoder(f)
	d.DisallowUnknownFields()
	if e = d.Decode(&c); e != nil {
		return c, e
	}
	var trailing any
	if e = d.Decode(&trailing); e != io.EOF {
		return c, fmt.Errorf("configuration must contain exactly one JSON object")
	}
	resolve := func(p string) string {
		if p == "" || filepath.IsAbs(p) {
			return p
		}
		return filepath.Join(filepath.Dir(path), p)
	}
	c.Spectrum = resolve(c.Spectrum)
	c.Tracks = resolve(c.Tracks)
	c.TerrainDir = resolve(c.TerrainDir)
	c.GeoidGrid = resolve(c.GeoidGrid)
	return c, nil
}
func analyze(ctx context.Context, path, out string, o acoustic.Options, profile string) (*acoustic.Spectrum, error) {
	ctx = acoustic.WithAnalysisProgress(ctx, func(p acoustic.AnalysisProgress) {
		fmt.Fprintf(os.Stderr, "%s: %s, block %d/%d · %.0f–%.0f s\n", p.Profile, p.Stage, p.Block, p.Blocks, p.From, p.Until)
	})
	fmt.Fprintln(os.Stderr, "Decoding audio and computing whitened spectrogram...")
	var s *acoustic.Spectrum
	var detection acoustic.DetectionReport
	var err error
	if profile == acoustic.HybridProfile {
		s, detection, err = acoustic.AnalyzeHybrid(ctx, path)
	} else if profile == acoustic.ShortPassProfile {
		s, detection, err = acoustic.AnalyzeShortPass(ctx, path, o)
	} else {
		s, err = acoustic.Analyze(ctx, path, o)
		if err == nil {
			detection, err = s.DiscoverProfile(profile)
		}
	}
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(out, 0755); err != nil {
		return nil, err
	}
	if err = s.Save(filepath.Join(out, "spectrogram.bin")); err != nil {
		return nil, err
	}
	if err = s.PNG(filepath.Join(out, "spectrogram.png")); err != nil {
		return nil, err
	}
	if err = s.SVG(filepath.Join(out, "spectrogram.svg"), nil); err != nil {
		return nil, err
	}
	if err = search.JSON(filepath.Join(out, "detection.json"), detection); err != nil {
		return nil, err
	}
	events := detection.Events
	var visible []acoustic.Curve
	for _, e := range events {
		if e.Evidence != nil && e.Evidence.Level != "weak" {
			curve := acoustic.Curve{Label: "Measured ridge: " + e.Evidence.Level + "; aircraft unconfirmed"}
			for _, p := range e.Ridge {
				curve.Points = append(curve.Points, [2]float64{p.Time, p.Hz})
			}
			visible = append(visible, curve)
		}
	}
	if err = s.SVG(filepath.Join(out, "curves.svg"), visible); err != nil {
		return nil, err
	}
	if err = search.JSON(filepath.Join(out, "events.json"), events); err != nil {
		return nil, err
	}
	metadata := map[string]any{"source": path, "frames": len(s.Rows), "first_frame_center_s": s.Time(0), "last_frame_center_s": s.Time(len(s.Rows) - 1), "hop_s": s.Step(), "frequency_bin_hz": float64(s.SampleRate) / float64(s.Window), "min_hz": s.Hz(0), "max_hz": s.Hz(len(s.Rows[0]) - 1), "channel": o.Channel, "note": "PNG: time left to right; frequency bottom to top; brightness is whitened spectral power. Events are unverified proposals; review and adjust before locating."}
	metadata["detector_profile"] = profile
	metadata["sample_rate_hz"] = s.SampleRate
	if err = search.JSON(filepath.Join(out, "analysis.json"), metadata); err != nil {
		return nil, err
	}
	fmt.Fprintf(os.Stderr, "Wrote %d frames and %d event proposals to %s\n", len(s.Rows), len(events), out)
	return s, nil
}
func locate(ctx context.Context, c search.Config, out string) error {
	s, err := acoustic.Load(c.Spectrum)
	if err != nil {
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
	fmt.Fprintf(os.Stderr, "Searching %d tracks, %d events, clock ±%.1fs...\n", len(ts), len(c.Events), c.ClockUncertainty)
	lastProgress := time.Now()
	report, err := search.RunWithProgress(ctx, c, s, ts, func(stage string, done, total int) {
		if done == 0 || done == total || time.Since(lastProgress) > 5*time.Second {
			fmt.Fprintf(os.Stderr, "%s: %d/%d locations\n", stage, done, total)
			lastProgress = time.Now()
		}
	})
	if err != nil {
		return err
	}
	if err = search.WriteReport(out, report, c, s, ts); err != nil {
		return err
	}
	fmt.Printf("%s: evaluated %d locations; results in %s\n", report.Status, report.Evaluated, out)
	fmt.Printf("Map: %s\n", filepath.Join(out, "map.html"))
	if len(report.Candidates) > 0 {
		p := report.Candidates[0]
		fmt.Printf("Best candidate %.6f, %.6f; score %.2f dB; clock offset %+.2fs\n", p.Lat, p.Lon, p.Score, p.ClockOffset)
		if math.Abs(p.ClockOffset) == c.ClockUncertainty && c.ClockUncertainty > 0 {
			fmt.Fprintln(os.Stderr, "Best clock offset is at the search boundary; consider expanding the window.")
		}
	}
	return nil
}
