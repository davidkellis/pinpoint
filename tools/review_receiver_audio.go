//go:build ignore

// Direct spectral flight review at a supplied receiver and a fixed shared clock.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"pinpoint/internal/acoustic"
	"pinpoint/internal/geo"
	"pinpoint/internal/search"
	"pinpoint/internal/tracks"
)

func require(err error) {
	if err != nil {
		panic(err)
	}
}
func digest(path string) string {
	f, e := os.Open(path)
	require(e)
	defer f.Close()
	h := sha256.New()
	_, e = io.Copy(h, f)
	require(e)
	return hex.EncodeToString(h.Sum(nil))
}
func saveReview(path string, v any) {
	b, e := json.MarshalIndent(v, "", "  ")
	require(e)
	require(os.WriteFile(path, b, 0600))
}

func preview(s *acoustic.Spectrum, path string, m search.GuidedMatch) {
	lo, hi := math.Inf(1), 0.
	curve := acoustic.Curve{Label: "Predicted trajectory and training-selected source tone; identity remains unconfirmed"}
	for _, p := range m.Curve {
		lo = math.Min(lo, p.Hz)
		hi = math.Max(hi, p.Hz)
		curve.Points = append(curve.Points, [2]float64{p.Time, p.Hz})
	}
	if !geo.Finite(lo) || hi <= 0 {
		return
	}
	padding := math.Max(20, (hi-lo)*.25)
	df := float64(s.SampleRate) / float64(s.Window)
	x0 := max(0, int(math.Floor((m.Start-s.Time(0))/s.Step())))
	x1 := min(len(s.Rows)-1, int(math.Ceil((m.End-s.Time(0))/s.Step())))
	y0 := max(0, int(math.Floor((lo-padding-s.Hz(0))/df)))
	y1 := min(len(s.Rows[0])-1, int(math.Ceil((hi+padding-s.Hz(0))/df)))
	if x1 <= x0 || y1 <= y0 {
		return
	}
	crop := *s
	crop.TimeOffset = s.TimeOffset + float64(x0)*s.Step()
	crop.FirstBin += y0
	crop.Rows = make([][]float32, x1-x0+1)
	for i := range crop.Rows {
		crop.Rows[i] = s.Rows[x0+i][y0 : y1+1]
	}
	require(crop.SVG(path, []acoustic.Curve{curve}))
}

func main() {
	for _, arg := range os.Args[1:] {
		if strings.HasPrefix(arg, "-") && !strings.HasPrefix(arg, "--") {
			panic("use two-hyphen options")
		}
	}
	config := flag.String("config", "", "private receiver, recording windows and source paths")
	out := flag.String("out", "work/flight-history-review", "review output root")
	mode := flag.String("mode", "both", "long (saved spectrum), short (0.5-second FFT), or both")
	clock := flag.Float64("clock-offset", 0, "one fixed correction for all flights, seconds")
	full := flag.Bool("full-curves", false, "require 120-second windows rather than selecting 40 or 120 s on training data")
	shortWindow := flag.Float64("short-window", .5, "scoring FFT duration; alias of --fft-window")
	flag.Float64Var(shortWindow, "fft-window", .5, "scoring FFT duration, 0.0625..4 seconds; power-of-two sample count")
	rate := flag.Int("sample-rate", 16384, "scoring spectrum sample rate")
	minHz := flag.Float64("min-hz", 40, "lowest measured frequency")
	maxHz := flag.Float64("max-hz", 6500, "highest measured frequency")
	sourceStep := flag.Float64("source-step", 2, "source-pitch search step in Hz")
	reviewWindows := flag.String("review-windows", "", "optional JSON array of audio-selected intervals; controls can use the rest of the recording")
	hopFraction := flag.Float64("hop-fraction", .5, "FFT hop/window ratio, 0.5 or 1; adjacent windows reduce long-recording scoring memory without truncating the audio")
	cached := flag.String("cached-spectrum", "", "reuse one measured spectrum after verifying its audio SHA-256 sidecar; single recording/profile only")
	smoothStep := flag.Float64("smooth-track-step", 0, "optional 0.05..2 second Hermite interpolation; 0 disables")
	reportedMotion := flag.Bool("reported-motion", false, "use retained ADS-B ground speed/true track/geometric rate with --smooth-track-step")
	linearControl := flag.Bool("linear-control", true, "compare linear pitch changes on the same audio as well as steady tones and wrong-time controls")
	flag.Parse()
	if !geo.Finite(*smoothStep) || *smoothStep != 0 && (*smoothStep < .05 || *smoothStep > 2) || *reportedMotion && *smoothStep == 0 {
		panic("invalid smoothing options")
	}
	if *config == "" || *mode != "long" && *mode != "short" && *mode != "both" {
		panic("supply --config and --mode long|short|both")
	}
	window := int(math.Round(float64(*rate) * *shortWindow))
	if !geo.Finite(*shortWindow) || *shortWindow < .0625 || *shortWindow > 4 || window < 256 || window > 65536 || window&(window-1) != 0 || math.Abs(float64(window)/float64(*rate)-*shortWindow) > 1e-9 || *rate < 1000 || *rate > 48000 || !geo.Finite(*minHz) || !geo.Finite(*maxHz) || *minHz < 0 || *maxHz <= *minHz || *maxHz >= float64(*rate)/2 || !geo.Finite(*sourceStep) || *sourceStep <= 0 {
		panic("invalid --short-window")
	}
	if *hopFraction != .125 && *hopFraction != .25 && *hopFraction != .5 && *hopFraction != 1 {
		panic("--hop-fraction must be 0.125, 0.25, 0.5 or 1")
	}
	b, e := os.ReadFile(*config)
	require(e)
	var c struct {
		Lat        float64 `json:"lat"`
		Lon        float64 `json:"lon"`
		Terrain    string  `json:"terrain_dir"`
		Geoid      string  `json:"geoid_file"`
		Recordings []struct {
			ID       string `json:"id"`
			Audio    string `json:"audio"`
			Start    string `json:"start"`
			Tracks   string `json:"tracks"`
			Analysis string `json:"analysis"`
		} `json:"recordings"`
	}
	require(json.Unmarshal(b, &c))
	if *cached != "" && (len(c.Recordings) != 1 || *mode == "both") {
		panic("--cached-spectrum requires one recording/profile")
	}
	if len(c.Recordings) == 0 {
		panic("no configured recordings")
	}
	t := geo.Terrain{Dir: c.Terrain}
	ground, e := t.Height(c.Lat, c.Lon)
	require(e)
	geoid, e := geo.LoadGeoid(c.Geoid)
	require(e)
	und, e := geoid.Height(c.Lat, c.Lon)
	require(e)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	ctx, timeout := context.WithTimeout(ctx, 45*time.Minute)
	defer timeout()
	o := search.DefaultGuidedOptions()
	o.MaxHz = math.Min(6000, 2**maxHz)
	o.StepHz = *sourceStep
	o.FullCurves = *full
	o.LinearControls = *linearControl
	if *reviewWindows != "" {
		data, e := os.ReadFile(*reviewWindows)
		require(e)
		require(json.Unmarshal(data, &o.ReviewIntervals))
	}
	exe, e := os.Executable()
	require(e)
	exeSHA := digest(exe)
	decoder, e := exec.Command("ffmpeg", "-version").Output()
	require(e)
	for _, r := range c.Recordings {
		if r.ID == "" || filepath.Base(r.ID) != r.ID {
			panic("invalid recording ID")
		}
		f, e := os.Open(r.Tracks)
		require(e)
		ts, e := tracks.Read(f)
		f.Close()
		require(e)
		var smoothing []tracks.SmoothingReport
		if *smoothStep > 0 {
			n := 0
			for i := range ts {
				v, r, e := ts[i].Smooth(tracks.SmoothingOptions{Step: *smoothStep, MaxGap: 30, MaxAcceleration: 20, UseKinematics: *reportedMotion})
				require(e)
				ts[i] = v
				smoothing = append(smoothing, r)
				n += r.ReportedHorizontal
			}
			if *reportedMotion && n == 0 {
				panic("no reported horizontal motion in supplied tracks")
			}
		}
		dir := filepath.Join(*out, r.ID)
		require(os.MkdirAll(dir, 0700))
		h := search.ReceiverHypothesis{Lat: c.Lat, Lon: c.Lon, Height: ground + und + 1.5, RecordingStart: r.Start, Clock: *clock, SoundSpeed: 343, MaxGap: 30}
		audioSHA := digest(r.Audio)
		for _, profile := range []string{"long", "short"} {
			if *mode != "both" && *mode != profile {
				continue
			}
			var s *acoustic.Spectrum
			source := filepath.Join(r.Analysis, "spectrogram.bin")
			if *cached != "" {
				source = *cached
				b, e := os.ReadFile(source + ".inputs.json")
				require(e)
				var proof struct {
					AudioSHA string `json:"audio_sha256"`
				}
				require(json.Unmarshal(b, &proof))
				if proof.AudioSHA != audioSHA {
					panic("cached spectrum belongs to different audio")
				}
				s, e = acoustic.Load(source)
				require(e)
			} else if profile == "short" {
				source = filepath.Join(dir, "receiver-short-spectrum.bin")
				opts := acoustic.Options{Rate: *rate, Window: window, Hop: int(float64(window) * *hopFraction), MinHz: *minHz, MaxHz: *maxHz}
				key := map[string]any{"audio_sha256": audioSHA, "options": opts, "decoder": strings.Split(string(decoder), "\n")[0], "executable_sha256": exeSHA}
				keyBytes, e := json.Marshal(key)
				require(e)
				previous, _ := os.ReadFile(source + ".inputs.json")
				if string(previous) == string(keyBytes) {
					s, e = acoustic.Load(source)
				} else {
					e = fmt.Errorf("new spectrum inputs")
				}
				if e != nil {
					fmt.Println(r.ID, "computing whole-recording spectrum, FFT seconds", *shortWindow)
					s, e = acoustic.Analyze(ctx, r.Audio, opts)
					require(e)
					require(s.Save(source))
					require(os.WriteFile(source+".inputs.json", keyBytes, 0600))
				}
			} else {
				s, e = acoustic.Load(source)
				require(e)
			}
			o.MaxHz = math.Min(6000, 2*s.Hz(len(s.Rows[0])-1))
			fmt.Println(r.ID, profile, "direct spectral review at fixed clock", *clock)
			review, e := search.ReviewReceiver(ctx, h, s, ts, o)
			require(e)
			saveReview(filepath.Join(dir, "receiver-review-"+profile+".json"), review)
			for i, m := range review.Matches {
				preview(s, filepath.Join(dir, fmt.Sprintf("receiver-%s-%03d.svg", profile, i)), m)
			}
			saveReview(filepath.Join(dir, "receiver-review-"+profile+"-inputs.json"), map[string]any{"smooth_track_step_s": *smoothStep, "reported_motion": *reportedMotion, "smoothing": smoothing, "fft_window_s": float64(s.Window) / float64(s.SampleRate), "hop_s": s.Step(), "measured_min_hz": s.Hz(0), "measured_max_hz": s.Hz(len(s.Rows[0]) - 1), "config": *config, "config_sha256": digest(*config), "audio": r.Audio, "audio_sha256": audioSHA, "tracks": r.Tracks, "tracks_sha256": digest(r.Tracks), "spectrum": source, "spectrum_sha256": digest(source), "executable": exe, "executable_sha256": exeSHA, "created_utc": time.Now().UTC(), "scope": "Supplied/assumed stationary receiver and fixed shared clock; no automatic detection prerequisite or location inference. Resolution profiles are alternative measurements, not independent aircraft."})
			fmt.Printf("%s %s · %d modeled windows / %d experimental support\n", r.ID, profile, len(review.Matches), review.Supported)
		}
	}
}
