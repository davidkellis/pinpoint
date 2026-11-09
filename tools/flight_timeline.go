//go:build ignore

// Export flight encounters at a supplied receiver for supervised audio review.
// This deliberately does not estimate the receiver's position or audibility.
package main

import (
	"compress/gzip"
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

	"pinpoint/internal/acoustic"
	"pinpoint/internal/geo"
	"pinpoint/internal/tracks"
)

type timelineRecording struct {
	ID       string  `json:"id"`
	Audio    string  `json:"audio"`
	Start    string  `json:"start"`
	Duration float64 `json:"duration_s"`
	Tracks   string  `json:"tracks"`
	Analysis string  `json:"analysis,omitempty"`
	Context  string  `json:"context_status,omitempty"`
}
type timelineConfig struct {
	Lat        float64             `json:"lat"`
	Lon        float64             `json:"lon"`
	Terrain    string              `json:"terrain_dir"`
	Geoid      string              `json:"geoid_file"`
	Cache      string              `json:"trace_cache"`
	Recordings []timelineRecording `json:"recordings"`
}
type timelinePass struct {
	Track        string   `json:"track_id"`
	ICAO         string   `json:"icao"`
	Type         string   `json:"type_code"`
	Engine       string   `json:"engine_class"`
	Registration string   `json:"registration,omitempty"`
	Callsign     string   `json:"callsign,omitempty"`
	Description  string   `json:"description,omitempty"`
	Operator     string   `json:"operator,omitempty"`
	MetadataPath string   `json:"metadata_path,omitempty"`
	MetadataSHA  string   `json:"metadata_sha256,omitempty"`
	UTC          string   `json:"arrival_cpa_utc"`
	CPA          float64  `json:"predicted_audio_cpa_s"`
	Delay        float64  `json:"propagation_delay_s"`
	Distance     float64  `json:"slant_m"`
	Speed        float64  `json:"speed_mps"`
	Start        float64  `json:"review_start_s"`
	End          float64  `json:"review_end_s"`
	Gap          *float64 `json:"gap_from_previous_encounter_s"`
	NearGap      *float64 `json:"gap_from_previous_near_encounter_s"`
	Partial      bool     `json:"partial_trajectory"`
}

func timelineCheck(e error) {
	if e != nil {
		panic(e)
	}
}
func timelineJSON(path string, v any) {
	b, e := json.MarshalIndent(v, "", "  ")
	timelineCheck(e)
	timelineCheck(os.WriteFile(path, b, 0600))
}
func timelineTime(sec float64) string {
	whole := math.Floor(sec)
	return time.Unix(int64(whole), int64((sec-whole)*1e9)).UTC().Format(time.RFC3339Nano)
}

// Retain separate local minima (including circuits), but merge fragments of
// the same aircraft within 120 s. Trace edges are hypotheses, not complete CPA.
func timelinePasses(ts []tracks.Track, obs geo.Vec, epoch, duration, radius, sound float64) []timelinePass {
	var possible []timelinePass
	for _, t := range ts {
		for i := 1; i < len(t.Points); i++ {
			a, b := t.Points[i-1], t.Points[i]
			dt := b.Time - a.Time
			if dt <= 0 || dt > 30 {
				continue
			}
			delta := b.Position.Sub(a.Position)
			speed := delta.Norm() / dt
			if speed < 10 || speed >= sound {
				continue
			}
			u := obs.Sub(a.Position).Dot(delta) / delta.Dot(delta)
			partial := false
			if u < 0 {
				if i > 1 && a.Time-t.Points[i-2].Time <= 30 && t.Points[i-2].Position.Sub(obs).Norm() < a.Position.Sub(obs).Norm() {
					continue
				}
				u = 0
				partial = i == 1 || a.Time-t.Points[i-2].Time > 30
			} else if u > 1 {
				if i+1 < len(t.Points) && t.Points[i+1].Time-b.Time <= 30 && t.Points[i+1].Position.Sub(obs).Norm() < b.Position.Sub(obs).Norm() {
					continue
				}
				u = 1
				partial = i == len(t.Points)-1 || t.Points[i+1].Time-b.Time > 30
			}
			d := a.Position.Add(delta.Scale(u)).Sub(obs).Norm()
			heard := a.Time + u*dt + d/sound
			if d > radius || heard < epoch || heard > epoch+duration {
				continue
			}
			half := math.Max(30, math.Min(120, 3*d/speed))
			cpa := heard - epoch
			possible = append(possible, timelinePass{Track: t.ID, ICAO: strings.Split(t.ID, "-")[0], Type: t.TypeCode, Engine: tracks.EngineClass(t.TypeCode), UTC: timelineTime(heard), CPA: cpa, Delay: d / sound, Distance: d, Speed: speed, Start: math.Max(0, cpa-half), End: math.Min(duration, cpa+half), Partial: partial})
		}
	}
	sort.Slice(possible, func(i, j int) bool { return possible[i].Distance < possible[j].Distance })
	var out []timelinePass
	byAircraft := map[string][]float64{}
	for _, p := range possible {
		duplicate := false
		for _, sec := range byAircraft[p.ICAO] {
			if math.Abs(sec-p.CPA) < 120 {
				duplicate = true
				break
			}
		}
		if duplicate {
			continue
		}
		out = append(out, p)
		byAircraft[p.ICAO] = append(byAircraft[p.ICAO], p.CPA)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CPA < out[j].CPA })
	var near *float64
	for i := range out {
		if i > 0 {
			gap := out[i].CPA - out[i-1].CPA
			out[i].Gap = &gap
		}
		if out[i].Distance <= 5000 {
			if near != nil {
				gap := out[i].CPA - *near
				out[i].NearGap = &gap
			}
			x := out[i].CPA
			near = &x
		}
	}
	return out
}

func timelineMetadata(p *timelinePass, epoch float64, cache string) {
	// Use emission time to select callsign. Header registration/type describe
	// the airframe; a callsign from an unrelated flight that day must not leak in.
	sec := epoch + p.CPA - p.Delay
	day := time.Unix(int64(sec), 0).UTC().Format("2006-01-02")
	path := filepath.Join(cache, "traces", day, p.ICAO+".json")
	b, e := os.ReadFile(path)
	if e != nil {
		return
	}
	hash := sha256.Sum256(b)
	p.MetadataPath = path
	p.MetadataSHA = hex.EncodeToString(hash[:])
	var r io.Reader = strings.NewReader(string(b))
	if len(b) > 2 && b[0] == 0x1f && b[1] == 0x8b {
		g, e := gzip.NewReader(r)
		if e != nil {
			return
		}
		defer g.Close()
		r = g
	}
	var tr struct {
		Registration string              `json:"r"`
		Description  string              `json:"desc"`
		Operator     string              `json:"ownOp"`
		Timestamp    float64             `json:"timestamp"`
		Trace        [][]json.RawMessage `json:"trace"`
	}
	if json.NewDecoder(r).Decode(&tr) != nil {
		return
	}
	p.Registration = tr.Registration
	p.Description = tr.Description
	p.Operator = tr.Operator
	best := 30.
	for _, row := range tr.Trace {
		if len(row) < 9 {
			continue
		}
		var dt float64
		if json.Unmarshal(row[0], &dt) != nil || math.Abs(tr.Timestamp+dt-sec) > best {
			continue
		}
		var m struct {
			Flight string `json:"flight"`
		}
		if json.Unmarshal(row[8], &m) == nil && strings.TrimSpace(m.Flight) != "" {
			best = math.Abs(tr.Timestamp + dt - sec)
			p.Callsign = strings.TrimSpace(m.Flight)
		}
	}
}

type historyPosition struct {
	ID       string  `json:"id"`
	Callsign string  `json:"callsign"`
	Lat      float64 `json:"latitude"`
	Lon      float64 `json:"longitude"`
	Time     float64 `json:"timestamp"`
	Millis   float64 `json:"timestampMs"`
	Ground   bool    `json:"onGround"`
}
type historyJoin struct {
	FR24     historyPosition `json:"fr24"`
	Snapshot string          `json:"snapshot"`
	Track    string          `json:"geometric_track_id,omitempty"`
	ICAO     string          `json:"geometric_icao,omitempty"`
	Callsign string          `json:"geometric_callsign,omitempty"`
	Type     string          `json:"geometric_type_code,omitempty"`
	Distance *float64        `json:"horizontal_difference_m,omitempty"`
	Status   string          `json:"status"`
}

func historyCompare(dir string, ts []tracks.Track, ps []timelinePass, epoch, duration float64) {
	files, e := filepath.Glob(filepath.Join(dir, "fr24", "[0-9]*.json"))
	timelineCheck(e)
	if len(files) == 0 {
		return
	}
	rows := []historyJoin{}
	for _, path := range files {
		b, e := os.ReadFile(path)
		timelineCheck(e)
		var snap struct {
			Status    string            `json:"status"`
			Requested float64           `json:"requested_timestamp_s"`
			Timestamp float64           `json:"playback_timestamp_s"`
			Aircraft  []historyPosition `json:"aircraft"`
		}
		timelineCheck(json.Unmarshal(b, &snap))
		if snap.Status != "captured" || snap.Requested != snap.Timestamp {
			continue
		}
		for _, a := range snap.Aircraft {
			stamp := a.Time
			if a.Millis > 0 {
				stamp = a.Millis / 1000
			}
			// Use aircraft observation time, never the live global server clock.
			if a.Ground || !geo.Finite(stamp) || !geo.Finite(a.Lat) || !geo.Finite(a.Lon) || math.Abs(a.Lat) > 90 || math.Abs(a.Lon) > 180 || stamp < epoch || stamp > epoch+duration || math.Abs(stamp-snap.Requested) > 90 {
				continue
			}
			r := historyJoin{FR24: a, Snapshot: path, Status: "no_geometric_match"}
			best, runner := math.Inf(1), math.Inf(1)
			bestIndex := -1
			bestICAO := ""
			for k, t := range ts {
				i := sort.Search(len(t.Points), func(i int) bool { return t.Points[i].Time >= stamp })
				if i == 0 || i == len(t.Points) {
					continue
				}
				x, y := t.Points[i-1], t.Points[i]
				gap := y.Time - x.Time
				if gap <= 0 || gap > 30 {
					continue
				}
				u := (stamp - x.Time) / gap
				lat := x.Lat + u*(y.Lat-x.Lat)
				lon := x.Lon + u*(y.Lon-x.Lon)
				d := geo.ECEF(a.Lat, a.Lon, 0).Sub(geo.ECEF(lat, lon, 0)).Norm()
				icao := strings.Split(t.ID, "-")[0]
				if d < best {
					if icao != bestICAO {
						runner = best
					}
					best = d
					bestIndex = k
					bestICAO = icao
				} else if icao != bestICAO && d < runner {
					runner = d
				}
			}
			if bestIndex >= 0 && best <= 1000 {
				t := ts[bestIndex]
				r.Track = t.ID
				r.ICAO = bestICAO
				r.Type = t.TypeCode
				r.Distance = &best
				r.Status = "position_only"
				near := math.Inf(1)
				for _, p := range ps {
					delta := math.Abs(epoch + p.CPA - p.Delay - stamp)
					if p.ICAO == bestICAO && delta < 300 && delta < near {
						near = delta
						r.Callsign = p.Callsign
					}
				}
				if runner < math.Max(1000, best+250) {
					r.Status = "ambiguous_position"
				} else if a.Callsign != "" && r.Callsign != "" {
					if strings.EqualFold(a.Callsign, r.Callsign) {
						r.Status = "consistent_callsign_and_position"
					} else {
						r.Status = "callsign_disagreement"
					}
				}
			}
			rows = append(rows, r)
		}
	}
	timelineJSON(filepath.Join(dir, "fr24-comparison.json"), map[string]any{"note": "FR24 is independently observed website playback. Association uses the aircraft's historical timestamp and horizontal proximity (<=1000 m), then checks the time-local ADSB.lol callsign. Position-only associations are hypotheses. FR24 map altitude is never used for acoustic geometry. Sparse viewport sampling can miss flights; providers can share underlying ADS-B messages.", "observations": rows})
}

type historyRidgeFit struct {
	Evidence string  `json:"source_evidence_level"`
	Event    int     `json:"event"`
	Start    float64 `json:"start_s"`
	End      float64 `json:"end_s"`
	Track    string  `json:"track_id"`
	Clock    float64 `json:"common_clock_correction_s"`
	F0       float64 `json:"training_source_hz"`
	Train    float64 `json:"training_rmse_hz"`
	Test     float64 `json:"reserved_rmse_hz"`
	Flat     float64 `json:"reserved_constant_rmse_hz"`
	Linear   float64 `json:"reserved_linear_rmse_hz"`
	Coverage float64 `json:"geometry_coverage"`
	Pass     bool    `json:"ridge_agreement"`
}

// Descriptive shape comparison of the already discovered strong ridges. No
// arbitrary pitch modulation is fitted. All events share each trial's clock.
// Separate blocks test the tone fitted on training samples; this is not the
// production sequence/spectral/null-control validation or an aircraft label.
func historyRidges(dir string, r timelineRecording, ts []tracks.Track, obs geo.Vec, epoch float64, includeReview bool) {
	path := filepath.Join(r.Analysis, "detection.json")
	if r.Analysis == "" {
		return
	}
	b, e := os.ReadFile(path)
	if e != nil {
		return
	}
	var d acoustic.DetectionReport
	timelineCheck(json.Unmarshal(b, &d))
	all := []historyRidgeFit{}
	for index, event := range d.Events {
		if event.Evidence == nil || event.Evidence.Level != "strong" && !(includeReview && event.Evidence.Level == "review") {
			continue
		}
		for _, t0 := range ts {
			if len(t0.Points) < 2 || t0.Points[len(t0.Points)-1].Time < epoch+event.Start-150 || t0.Points[0].Time > epoch+event.End+30 {
				continue
			}
			t := t0.ForObserver(obs, 343)
			for clock := -30.; clock <= 30; clock += 2 {
				type point struct {
					time, hz, factor float64
					test             bool
				}
				points := []point{}
				var ff, fy, sy, st, stt, sty float64
				expected, train := 0, 0
				for _, p := range event.Ridge {
					rel := p.Time - event.Start
					phase := math.Mod(rel, 10)
					if phase < 2 || phase >= 8 {
						continue
					}
					expected++
					factor, distance, ok := t.Received(obs, epoch+clock+p.Time, 343, 30)
					if !ok || distance > 15000 {
						continue
					}
					isTest := int(rel/10)%2 == 1
					points = append(points, point{rel, p.Hz, factor, isTest})
					if !isTest {
						ff += factor * factor
						fy += factor * p.Hz
						sy += p.Hz
						st += rel
						stt += rel * rel
						sty += rel * p.Hz
						train++
					}
				}
				if expected == 0 || train < 5 || float64(len(points))/float64(expected) < .95 || ff == 0 {
					continue
				}
				f0 := fy / ff
				flat := sy / float64(train)
				slope := 0.
				den := float64(train)*stt - st*st
				if den > 0 {
					slope = (float64(train)*sty - st*sy) / den
				}
				intercept := (sy - slope*st) / float64(train)
				var trainSq, testSq, flatSq, lineSq float64
				test := 0
				for _, p := range points {
					delta := p.hz - f0*p.factor
					if p.test {
						testSq += delta * delta
						flatSq += math.Pow(p.hz-flat, 2)
						lineSq += math.Pow(p.hz-intercept-slope*p.time, 2)
						test++
					} else {
						trainSq += delta * delta
					}
				}
				if test < 5 {
					continue
				}
				fit := historyRidgeFit{Evidence: event.Evidence.Level, Event: index + 1, Start: event.Start, End: event.End, Track: t.ID, Clock: clock, F0: f0, Train: math.Sqrt(trainSq / float64(train)), Test: math.Sqrt(testSq / float64(test)), Flat: math.Sqrt(flatSq / float64(test)), Linear: math.Sqrt(lineSq / float64(test)), Coverage: float64(len(points)) / float64(expected)}
				limit := math.Max(1.5, .012*f0)
				fit.Pass = fit.Train <= limit && fit.Test <= limit && fit.Test < fit.Flat*.9 && fit.Test < fit.Linear*.9
				all = append(all, fit)
			}
		}
	}
	// Each clock's event/track choice is based on training error alone. The
	// report retains every fit so a failed choice is not silently replaced.
	sort.SliceStable(all, func(i, j int) bool { return all[i].Train/all[i].F0 < all[j].Train/all[j].F0 })
	selected := []historyRidgeFit{}
	curves := []map[string]any{}
	seen := map[int]bool{}
	for _, fit := range all {
		if seen[fit.Event] {
			continue
		}
		seen[fit.Event] = true
		selected = append(selected, fit)
		var observed, predicted []acoustic.RidgeSample
		for _, t := range ts {
			if t.ID == fit.Track {
				for _, p := range d.Events[fit.Event-1].Ridge {
					factor, _, ok := t.Received(obs, epoch+fit.Clock+p.Time, 343, 30)
					if ok {
						observed = append(observed, p)
						predicted = append(predicted, acoustic.RidgeSample{Time: p.Time, Hz: factor * fit.F0})
					}
				}
				break
			}
		}
		curves = append(curves, map[string]any{"fit": fit, "observed": observed, "predicted": predicted})
	}
	common := []map[string]any{}
	for clock := -30.; clock <= 30; clock += 2 {
		chosen := map[int]bool{}
		used := map[string]bool{}
		accepted := []historyRidgeFit{}
		for _, fit := range all {
			if fit.Clock != clock || chosen[fit.Event] {
				continue
			}
			chosen[fit.Event] = true
			if !fit.Pass {
				continue
			}
			icao := strings.Split(fit.Track, "-")[0]
			if used[icao] {
				continue
			}
			overlap := false
			for _, other := range accepted {
				if fit.Start < other.End+4 && fit.End > other.Start-4 {
					overlap = true
					break
				}
			}
			if !overlap {
				accepted = append(accepted, fit)
				used[icao] = true
			}
		}
		common = append(common, map[string]any{"clock_s": clock, "distinct_nonoverlapping_ridge_agreements": len(accepted), "fits": accepted})
	}
	timelineJSON(filepath.Join(dir, "ridge-comparison.json"), map[string]any{"source": path, "include_review_curves": includeReview, "note": "Diagnostic of existing tonal proposals at the supplied or assumed static receiver. Original acoustic evidence tiers are retained and are not promoted. Source pitch is fitted on alternating 10-second training blocks with 2-second guards; other blocks assess shape. One shared clock grid (-30..30 s, step 2). Not spectral detection, a new aircraft label, silence evidence, independent validation, or blind localization. No event is shifted separately in a common-clock trial. All fits remain available; thresholds do not change production behavior. Per-event review winners can have different clocks; only common_clock_trials combine events, and overlapping sound/duplicate aircraft never count twice.", "fits": all, "training_selected_review_fits": selected, "review_curves": curves, "common_clock_trials": common})
}

func historyPlots(dir string, r timelineRecording, ps []timelinePass, spectrumPath string) {
	if r.Analysis == "" {
		return
	}
	if spectrumPath == "" {
		spectrumPath = filepath.Join(r.Analysis, "spectrogram.bin")
	}
	s, e := acoustic.Load(spectrumPath)
	timelineCheck(e)
	actual, _ := filepath.Abs(s.Source)
	expected, _ := filepath.Abs(r.Audio)
	if actual != expected {
		panic("plot spectrum belongs to another audio source")
	}
	plot := func(name string, start, end, low, high float64, curves []acoustic.Curve) {
		first := max(0, int(math.Floor((start-s.Time(0))/s.Step())))
		last := min(len(s.Rows)-1, int(math.Ceil((end-s.Time(0))/s.Step())))
		binSize := float64(s.SampleRate) / float64(s.Window)
		lo := max(0, int(math.Floor((low-s.Hz(0))/binSize)))
		hi := min(len(s.Rows[0])-1, int(math.Ceil((high-s.Hz(0))/binSize)))
		if last <= first || hi <= lo {
			return
		}
		crop := *s
		crop.TimeOffset = s.TimeOffset + float64(first)*s.Step()
		crop.FirstBin = s.FirstBin + lo
		crop.Rows = make([][]float32, last-first+1)
		for i := range crop.Rows {
			crop.Rows[i] = s.Rows[first+i][lo : hi+1]
		}
		timelineCheck(crop.SVG(filepath.Join(dir, name), curves))
	}
	b, e := os.ReadFile(filepath.Join(dir, "ridge-comparison.json"))
	if e == nil {
		var review struct {
			Curves []struct {
				Fit       historyRidgeFit        `json:"fit"`
				Observed  []acoustic.RidgeSample `json:"observed"`
				Predicted []acoustic.RidgeSample `json:"predicted"`
			} `json:"review_curves"`
		}
		timelineCheck(json.Unmarshal(b, &review))
		for _, c := range review.Curves {
			low, high := math.Inf(1), 0.
			curves := []acoustic.Curve{{Label: "Observed automatic tonal ridge"}, {Label: "Training-selected flight prediction; identity unconfirmed"}}
			for i, points := range [][]acoustic.RidgeSample{c.Observed, c.Predicted} {
				for _, p := range points {
					low = math.Min(low, p.Hz)
					high = math.Max(high, p.Hz)
					curves[i].Points = append(curves[i].Points, [2]float64{p.Time, p.Hz})
				}
			}
			if high <= 0 {
				continue
			}
			padding := math.Max(15, (high-low)*.15)
			plot(fmt.Sprintf("ridge-%d.svg", c.Fit.Event), c.Fit.Start-10, c.Fit.End+10, low-padding, high+padding, curves)
		}
	}
	if b, e := os.ReadFile(filepath.Join(r.Analysis, "detection.json")); e == nil {
		var d acoustic.DetectionReport
		timelineCheck(json.Unmarshal(b, &d))
		for i, event := range d.Events {
			if event.Evidence == nil || event.Evidence.Level != "strong" || len(event.Ridge) == 0 {
				continue
			}
			lo, hi := math.Inf(1), 0.
			curve := acoustic.Curve{Label: "Automatic tonal proposal; aircraft identity unconfirmed"}
			for _, p := range event.Ridge {
				lo = math.Min(lo, p.Hz)
				hi = math.Max(hi, p.Hz)
				curve.Points = append(curve.Points, [2]float64{p.Time, p.Hz})
			}
			padding := math.Max(20, (hi-lo)*.2)
			plot(fmt.Sprintf("proposal-%d.svg", i+1), event.Start-10, event.End+10, lo-padding, hi+padding, []acoustic.Curve{curve})
		}
	}
	timelineJSON(filepath.Join(dir, "plot-inputs.json"), map[string]any{"spectrum": spectrumPath, "source": s.Source, "window_s": float64(s.Window) / float64(s.SampleRate), "hop_s": s.Step(), "note": "Measured whitened spectral energy. Proposal overlays are automatic hypotheses, not identified aircraft."})
	jets := 0
	for _, p := range ps {
		if p.Engine != "J" || p.Distance > 5000 || p.Partial {
			continue
		}
		jets++
		if jets > 3 {
			break
		}
		// With no measured source tone, draw measured energy only. A guessed
		// synthetic pitch would misleadingly look like an acoustic match.
		name := fmt.Sprintf("jet-%s-%d", p.ICAO, int(p.CPA))
		plot(name+"-low.svg", p.Start, p.End, 40, 800, nil)
		plot(name+"-high.svg", p.Start, p.End, 800, 4000, nil)
	}
}

func main() {
	config := flag.String("config", "", "private receiver/recording/track configuration")
	out := flag.String("out", "work/flight-history-review", "new output directory")
	radius := flag.Float64("radius", 15000, "slant radius in metres; not an audibility claim")
	ridges := flag.Bool("ridges", false, "compare existing automatic strong ridges at supplied receiver/common clocks")
	includeReview := flag.Bool("include-review-curves", false, "diagnostic only: also compare review-tier curves without promoting their labels")
	plots := flag.Bool("plots", false, "export cropped measured spectrograms and reviewed curve overlays")
	plotSpectrum := flag.String("plot-spectrum", "", "optional measured spectrum for display; must belong to the same recording")
	flag.Parse()
	if *config == "" || !geo.Finite(*radius) || *radius <= 0 || *radius > 50000 {
		panic("supply --config and a radius in (0,50000]")
	}
	b, e := os.ReadFile(*config)
	timelineCheck(e)
	var c timelineConfig
	timelineCheck(json.Unmarshal(b, &c))
	if !geo.Finite(c.Lat) || !geo.Finite(c.Lon) || math.Abs(c.Lat) > 90 || math.Abs(c.Lon) > 180 || len(c.Recordings) == 0 {
		panic("invalid receiver/recordings")
	}
	terrain := geo.Terrain{Dir: c.Terrain}
	h, e := terrain.Height(c.Lat, c.Lon)
	timelineCheck(e)
	g, e := geo.LoadGeoid(c.Geoid)
	timelineCheck(e)
	n, e := g.Height(c.Lat, c.Lon)
	timelineCheck(e)
	obs := geo.ECEF(c.Lat, c.Lon, h+n)
	timelineCheck(os.MkdirAll(*out, 0700))
	for _, r := range c.Recordings {
		if r.ID == "" || filepath.Base(r.ID) != r.ID || !geo.Finite(r.Duration) || r.Duration <= 0 {
			panic("invalid recording")
		}
		start, e := time.Parse(time.RFC3339, r.Start)
		timelineCheck(e)
		epoch := float64(start.UnixNano()) / 1e9
		f, e := os.Open(r.Tracks)
		timelineCheck(e)
		ts, e := tracks.Read(f)
		f.Close()
		timelineCheck(e)
		ps := timelinePasses(ts, obs, epoch, r.Duration, *radius, 343)
		for i := range ps {
			timelineMetadata(&ps[i], epoch, c.Cache)
		}
		dir := filepath.Join(*out, r.ID)
		timelineCheck(os.MkdirAll(dir, 0700))
		timelineJSON(filepath.Join(dir, "encounters.json"), map[string]any{"recording": r, "start_utc": start.UTC().Format(time.RFC3339), "end_utc": timelineTime(epoch + r.Duration), "receiver": []float64{c.Lat, c.Lon, h + n}, "clock_correction_s": 0, "clock_uncertainty_s": 30, "sound_speed_mps": 343, "max_gap_s": 30, "radius_m": *radius, "geometry_source": "ADSB.lol geometric trajectories; FR24 metadata, when separately joined, does not replace height", "note": "Supervised review at a supplied receiver. Predicted arrival times and relative gaps are fixed by flight geometry. Not blind localization, an audibility prediction, or acoustic aircraft identification. Partial traces and review windows are explicitly marked. A common clock correction shifts every encounter equally.", "encounters": ps})
		historyCompare(dir, ts, ps, epoch, r.Duration)
		if *ridges {
			historyRidges(dir, r, ts, obs, epoch, *includeReview)
		}
		if *plots {
			historyPlots(dir, r, ps, *plotSpectrum)
		}
		fmt.Printf("%s: %d predicted encounters\n", r.ID, len(ps))
	}
}
