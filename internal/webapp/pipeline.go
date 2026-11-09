package webapp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"time"

	"pinpoint/internal/acoustic"
	"pinpoint/internal/demo"
	"pinpoint/internal/geo"
	"pinpoint/internal/search"
	"pinpoint/internal/tracks"
)

type SearchRequest struct {
	IncludeWeak      bool                    `json:"include_weak_candidates,omitempty"`
	FinalGrid        float64                 `json:"final_grid_m,omitempty"`
	ExcludeJets      *bool                   `json:"exclude_known_jets,omitempty"`
	Silence          []search.SilentInterval `json:"silence_intervals,omitempty"`
	MatchMode        string                  `json:"match_mode,omitempty"`
	State            string                  `json:"state,omitempty"`
	LocalStart       string                  `json:"local_start,omitempty"`
	TimeZone         string                  `json:"time_zone,omitempty"`
	Moving           bool                    `json:"moving,omitempty"`
	Automatic        bool                    `json:"automatic,omitempty"`
	RecordingStart   string                  `json:"recording_start"`
	Bounds           geo.Bounds              `json:"bounds"`
	Height           *float64                `json:"observer_height_ellipsoid_m"`
	ClockUncertainty float64                 `json:"clock_uncertainty_s"`
	Source           string                  `json:"source"`
	Grid             float64                 `json:"grid_m"`
	Refine           float64                 `json:"refine_m"`
	Events           []acoustic.Event        `json:"events,omitempty"`
}

func (r SearchRequest) independent() bool {
	if r.MatchMode != "" {
		return r.MatchMode == "independent"
	}
	return r.Automatic || r.Moving
}

func defaultRequest() SearchRequest {
	return SearchRequest{MatchMode: "consensus", Automatic: true, ClockUncertainty: 30, Source: "archive", Grid: 200, Refine: 50, FinalGrid: 10}
}
func (s *Server) analyze(ctx context.Context, j *job, preset []acoustic.Event) error {
	ctx = acoustic.WithAnalysisProgress(ctx, func(p acoustic.AnalysisProgress) {
		j.update(func(i *JobInfo) {
			i.Phase = "audio"
			i.Message = fmt.Sprintf("%s: %s, block %d/%d · %.0f–%.0f s of the recording", p.Profile, p.Stage, p.Block, p.Blocks, p.From, p.Until)
			i.Completed = int64(p.Block - 1)
			i.Total = int64(p.Blocks)
		})
	})
	j.phase("audio", "Decoding audio and measuring its frequency changes")
	if p, _ := detectorProfile(j.snapshot().DetectorProfile); p != "standard" {
		return s.analyzeProfile(ctx, j, p)
	}
	spectrum, secondary, detection, err := acoustic.AnalyzeWithSpectra(ctx, filepath.Join(j.dir, "audio"))
	if err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	spectrum.Source = j.snapshot().Filename
	if err = spectrum.Save(filepath.Join(j.dir, "spectrogram.bin")); err != nil {
		return err
	}
	if err = spectrum.SVG(filepath.Join(j.dir, "spectrogram.svg"), nil); err != nil {
		return err
	}
	if secondary != nil {
		if err = secondary.Save(filepath.Join(j.dir, "detection-spectrum.bin")); err != nil {
			return err
		}
	}
	return s.storeDetection(ctx, j, spectrum, detection, preset)
}

// Reuse the decoded spectra when the detector changes. This does not start a
// location search or discard the recording's time, area, or flight dataset.
func (s *Server) reanalyze(w http.ResponseWriter, r *http.Request) {
	j := s.getJob(w, r)
	if j == nil {
		return
	}
	if busy(j.snapshot().State) {
		problem(w, 409, "This recording is already being processed.")
		return
	}
	profile, _ := detectorProfile(j.snapshot().DetectorProfile)
	var req struct {
		Profile string `json:"detector_profile"`
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	d.DisallowUnknownFields()
	if err := d.Decode(&req); err != nil && err != io.EOF {
		problem(w, 400, "Invalid analysis profile request.")
		return
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		problem(w, 400, "Expected one analysis profile request.")
		return
	}
	if req.Profile != "" {
		var err error
		profile, err = detectorProfile(req.Profile)
		if err != nil {
			problem(w, 400, err.Error())
			return
		}
	}
	previous, _ := detectorProfile(j.snapshot().DetectorProfile)
	if !s.start(j, "analyzing", func(ctx context.Context) error {
		j.update(func(i *JobInfo) { i.DetectorProfile = profile })
		if profile != "standard" || previous != profile {
			return s.analyze(ctx, j, nil)
		}
		spectrum, err := acoustic.Load(filepath.Join(j.dir, "spectrogram.bin"))
		if err != nil {
			return s.analyze(ctx, j, nil)
		}
		return s.analyzeCurves(ctx, j, spectrum, nil)
	}) {
		problem(w, 409, "This recording is already being processed.")
		return
	}
	reply(w, http.StatusAccepted, j.snapshot())
}

func detectionMessage(d acoustic.DetectionReport) string {
	return fmt.Sprintf("%d strong acoustic curves; %d need review. %d bound closest approach. Strong partial curves can also share the recording clock. These are sound candidates, not confirmed aircraft.", d.Strong, d.Review, d.Joint)
}

func (s *Server) analyzeCurves(ctx context.Context, j *job, spectrum *acoustic.Spectrum, preset []acoustic.Event) error {
	j.phase("events", "Tracking tonal curves across the whole recording and checking their evidence")
	detection, err := s.detectRecording(ctx, j, spectrum)
	if err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	return s.storeDetection(ctx, j, spectrum, detection, preset)
}

func (s *Server) storeDetection(ctx context.Context, j *job, spectrum *acoustic.Spectrum, detection acoustic.DetectionReport, preset []acoustic.Event) error {
	var err error
	selected := detection.Events
	if preset != nil {
		selected = preset
		detection.SetEvents(preset)
	}
	if err = search.JSON(filepath.Join(j.dir, "detection.json"), detection); err != nil {
		return err
	}
	if err = search.JSON(filepath.Join(j.dir, "events.json"), selected); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	j.update(func(i *JobInfo) {
		i.State = "ready"
		i.Phase = "ready"
		i.Events = selected
		i.Detection = &detection
		i.FirstTime = spectrum.Time(0)
		i.LastTime = spectrum.Time(len(spectrum.Rows) - 1)
		i.Message = detectionMessage(detection)
		if len(selected) == 0 {
			i.Message = "No clear flyover curves found. Try another recording or add an event using the spectrogram."
		}
	})
	return nil
}
func (s *Server) search(w http.ResponseWriter, r *http.Request) {
	j := s.getJob(w, r)
	if j == nil {
		return
	}
	if busy(j.snapshot().State) {
		problem(w, 409, "This recording is already being processed.")
		return
	}
	request := defaultRequest()
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	d.DisallowUnknownFields()
	if err := d.Decode(&request); err != nil {
		problem(w, 400, "Invalid search settings: "+err.Error())
		return
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		problem(w, 400, "Expected one search request.")
		return
	}
	spectrum, err := acoustic.Load(filepath.Join(j.dir, "spectrogram.bin"))
	if err != nil {
		problem(w, 409, "Audio analysis has not completed. Upload the recording again.")
		return
	}
	if request.Automatic {
		var detection acoustic.DetectionReport
		raw, e := os.ReadFile(filepath.Join(j.dir, "detection.json"))
		if e != nil || json.Unmarshal(raw, &detection) != nil || detection.Version != detectorVersion(j.snapshot().DetectorProfile) {
			if p, _ := detectorProfile(j.snapshot().DetectorProfile); p != "standard" {
				problem(w, 409, "The detector has changed. Click Reanalyze recording before searching.")
				return
			}
			detection, err = s.detectRecording(r.Context(), j, spectrum)
			if err != nil {
				problem(w, 500, "Could not refresh audio detection: "+err.Error())
				return
			}
			if err = r.Context().Err(); err != nil {
				problem(w, 408, err.Error())
				return
			}
			if err = search.JSON(filepath.Join(j.dir, "detection.json"), detection); err != nil {
				problem(w, 500, err.Error())
				return
			}
			if err = search.JSON(filepath.Join(j.dir, "events.json"), detection.Events); err != nil {
				problem(w, 500, err.Error())
				return
			}
		}
		request.Events = acoustic.SearchEvents(detection.Events, !request.independent(), request.IncludeWeak)
		j.update(func(i *JobInfo) {
			i.Events = detection.Events
			i.Detection = &detection
			i.Message = detectionMessage(detection)
		})
	} else if request.Events == nil {
		request.Events = j.snapshot().Events
	}
	if err = normalizeRequest(&request); err != nil {
		problem(w, 400, err.Error())
		return
	}
	c, err := s.configuration(j, request, spectrum)
	if err != nil {
		problem(w, 400, err.Error())
		return
	}
	if !s.start(j, "searching", func(ctx context.Context) error {
		j.update(func(i *JobInfo) { i.Params = &request })
		return s.find(ctx, j, request, c, spectrum)
	}) {
		problem(w, 409, "This recording is already being processed.")
		return
	}
	reply(w, 202, j.snapshot())
}
func (s *Server) configuration(j *job, r SearchRequest, spectrum *acoustic.Spectrum) (search.Config, error) {
	c := search.DefaultConfig()
	c.RecordingStart = r.RecordingStart
	c.Bounds = r.Bounds
	c.RegionMask = r.State
	if nationwide, _ := geo.StateBounds(""); r.State == "" && r.Bounds == nationwide {
		c.RegionMask = "CONUS"
	}
	c.ObserverHeight = r.Height
	c.ClockUncertainty = r.ClockUncertainty
	c.Grid = r.Grid
	c.Refine = r.Refine
	c.FinalGrid = r.FinalGrid
	if c.Refine == 0 {
		c.FinalGrid = 0
	}
	c.Workers = s.options.Workers
	c.Events = r.Events
	c.ExcludeJets = r.ExcludeJets != nil && *r.ExcludeJets
	c.Silence = r.Silence
	if r.MatchMode == "joint" {
		c.Staged = true
		c.DenseGrid = math.Min(100, c.Grid)
	}
	c.Corridors = true
	c.MaxGridPoints = 2000000
	c.OmitUnmodeled = true
	if c.ClockUncertainty > 60 {
		c.ClockStep = math.Ceil(2 * c.ClockUncertainty / 2000)
	}
	if c.Staged {
		c.ClockStep = math.Max(.25, 2*c.ClockUncertainty/2000)
	}
	if r.Height == nil {
		c.TerrainDir = filepath.Join(s.options.DataDir, "cache", "terrain")
		c.GeoidGrid = filepath.Join(s.options.DataDir, "cache", "egm96_15.gtx")
	}
	c.Spectrum = filepath.Join(j.dir, "spectrogram.bin")
	c.Tracks = filepath.Join(j.dir, "search-tracks.csv")
	if r.MatchMode != "" && r.MatchMode != "independent" && r.MatchMode != "joint" && r.MatchMode != "consensus" {
		return c, fmt.Errorf("Choose independent, joint or consensus matching.")
	}
	if (r.MatchMode == "joint" || r.MatchMode == "consensus") && r.Moving {
		return c, fmt.Errorf("Joint and consensus matching assume one stationary microphone position. Use independent matching for a walk.")
	}
	if r.MatchMode == "consensus" {
		if len(r.Silence) > 0 {
			return c, fmt.Errorf("Consensus search checks loudness automatically; reviewed silence intervals apply to joint matching only.")
		}
		events, _ := consensusCurves(r, j.snapshot().Events)
		if len(events) < 2 {
			return c, fmt.Errorf("Consensus search needs at least two strong curves below %.0f Hz; this selection has %d. Review the detected curves or choose another matching method.", acoustic.DefaultLocatableMaxHz, len(events))
		}
		c.Events = events
		if r.Source == "upload" && !j.snapshot().HasTracks {
			return c, fmt.Errorf("Upload a flight-track CSV before starting the search.")
		}
		return c, nil
	}
	if len(r.Events) == 0 {
		return c, fmt.Errorf("No flyovers pass the acoustic evidence checks for this matching mode. Review the fragments, select events manually, or enable weak-fragment diagnostics. No aircraft or location has been confirmed.")
	}
	if r.MatchMode == "joint" && len(r.Events) < 2 {
		return c, fmt.Errorf("Joint matching needs at least two usable flyover curves. Strong partial curves can share the recording clock without a measured CPA. Review the detected curves or choose independent screening. No independent result is substituted for a joint result.")
	}
	if r.ClockUncertainty > 3600 {
		return c, fmt.Errorf("Use a recording start time within one hour. A narrower window reduces ambiguous matches.")
	}
	if r.independent() {
		if len(r.Silence) > 0 {
			return c, fmt.Errorf("Reviewed silence intervals require stationary joint matching.")
		}
		for k, event := range c.Events {
			one := c
			one.Events = []acoustic.Event{event}
			if _, err := one.Validate(spectrum); err != nil {
				return c, fmt.Errorf("event %d: %w", k+1, err)
			}
		}
	} else {
		validate := c.Validate
		if r.Automatic && !j.snapshot().Synthetic {
			validate = func(s *acoustic.Spectrum) (float64, error) { return search.ValidateAutomatic(c, s) }
		}
		if _, err := validate(spectrum); err != nil {
			return c, err
		}
	}
	switch r.Source {
	case "archive":
	case "upload":
		if !j.snapshot().HasTracks {
			return c, fmt.Errorf("Upload a flight-track CSV before starting the search.")
		}
	case "local":
		if s.options.TracksFile == "" && s.options.TraceDir == "" {
			return c, fmt.Errorf("No local flight dataset is configured.")
		}
	default:
		return c, fmt.Errorf("Choose a flight-data source.")
	}
	return c, nil
}
func (s *Server) find(ctx context.Context, j *job, r SearchRequest, c search.Config, spectrum *acoustic.Spectrum) error {
	var ts []tracks.Track
	var err error
	if r.independent() {
		j.initEvents(r.Events)
		defer j.finishPendingEvents()
	}
	if r.Source == "upload" || r.Source == "local" && s.options.TracksFile != "" {
		j.phase("tracks", "Reading aircraft trajectories")
		path := filepath.Join(j.dir, "tracks.csv")
		if r.Source == "local" {
			path = s.options.TracksFile
		}
		c.Tracks = path
		f, e := os.Open(path)
		if e != nil {
			return e
		}
		ts, err = tracks.Read(f)
		f.Close()
		if err != nil {
			return err
		}
	} else {
		a, b := search.RecordingFlightWindow(c, spectrum)
		if r.Automatic && !r.independent() && r.MatchMode != "consensus" {
			o := search.DefaultSequenceOptions(c)
			pad := 0.
			for _, shift := range o.ControlShifts {
				pad = math.Max(pad, math.Abs(shift))
			}
			a -= pad
			b += pad
		}
		padLat := 30000.0 / 111320
		padLon := padLat / math.Cos((c.Bounds.North+c.Bounds.South)*math.Pi/360)
		bounds := geo.Bounds{South: math.Max(-90, c.Bounds.South-padLat), North: math.Min(90, c.Bounds.North+padLat), West: math.Max(-180, c.Bounds.West-padLon), East: math.Min(180, c.Bounds.East+padLon)}
		o := tracks.ImportOptions{Start: a, End: b, MaxGap: c.MaxGap, Bounds: &bounds}
		if err = search.JSON(filepath.Join(j.dir, "flight-window.json"), map[string]any{"from": time.Unix(int64(a), 0).UTC().Format(time.RFC3339), "until": time.Unix(int64(b), 0).UTC().Format(time.RFC3339), "scope": "entire_recording"}); err != nil {
			return err
		}
		j.phase("importing", "Selecting flights near the search area and recording time")
		f, e := os.Create(c.Tracks)
		if e != nil {
			return e
		}
		if r.Source == "local" {
			_, err = tracks.ImportContext(ctx, s.options.TraceDir, f, o)
		} else if s.hasCachedArchives(a, b) {
			var archives [][]string
			archives, err = s.archives(ctx, j, a, b)
			if err == nil {
				_, err = tracks.ImportTarArchives(ctx, archives, f, o)
			}
		} else {
			var provenance tracks.ReplayStats
			provenance, err = tracks.Replay(ctx, filepath.Join(s.options.DataDir, "cache"), f, o, s.options.MaxDownloadBytes, func(message string, done, total int) {
				j.update(func(i *JobInfo) {
					i.Phase = "flight_history"
					i.Message = message
					i.Completed = int64(done)
					i.Total = int64(total)
				})
			})
			if e := search.JSON(filepath.Join(j.dir, "flight-data.json"), provenance); err == nil {
				err = e
			}
			j.update(func(i *JobInfo) { i.MissingTraces = len(provenance.Missing) })
		}
		ce := f.Close()
		if err == nil {
			err = ce
		}
		if err != nil {
			return err
		}
		f, e = os.Open(c.Tracks)
		if e != nil {
			return e
		}
		ts, err = tracks.Read(f)
		f.Close()
		if err != nil {
			return err
		}
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if r.independent() {
		return s.findEvents(ctx, j, r, c, spectrum, ts)
	}
	if r.MatchMode == "consensus" {
		return s.findConsensus(ctx, j, r, c, ts)
	}
	if r.Automatic && !j.snapshot().Synthetic {
		return s.findSequences(ctx, j, c, spectrum, ts)
	}
	j.update(func(i *JobInfo) { i.EventMaps = nil; i.ResultURL = ""; i.ResultStatus = "" })
	if err = s.prepareTerrain(ctx, j, c, ts); err != nil {
		return err
	}
	j.phase("scoring", fmt.Sprintf("Jointly comparing %d flyovers with %d aircraft tracks using one recording clock", len(c.Events), len(ts)))
	last := time.Time{}
	report, err := search.RunWithProgress(ctx, c, spectrum, ts, func(stage string, done, total int) {
		if done != 0 && done != total && time.Since(last) < 350*time.Millisecond {
			return
		}
		last = time.Now()
		j.update(func(i *JobInfo) {
			i.Phase = "scoring"
			i.Message = fmt.Sprintf("Scoring the %s grid: %d of %d locations", stage, done, total)
			i.Completed = int64(done)
			i.Total = int64(total)
		})
	})
	if err != nil {
		return err
	}
	if r.Automatic {
		report.Note += fmt.Sprintf(" Automatic selection used %d of %d detected ridges together; all other proposals remain available for review.", len(r.Events), len(j.snapshot().Events))
	}
	j.phase("mapping", "Plotting the location scores on the map")
	if err = search.WriteReport(j.dir, report, c, spectrum, ts); err != nil {
		return err
	}
	if err = search.JSON(filepath.Join(j.dir, "search.json"), c); err != nil {
		return err
	}
	j.update(func(i *JobInfo) {
		i.State = "complete"
		i.Phase = "complete"
		i.ResultStatus = report.Status
		i.ResultURL = "/api/jobs/" + i.ID + "/files/map.html"
		i.Message = "The location map is ready. Scores are candidate matches, not verified positions."
		if report.Status == "no_match" {
			i.Message = "No location passed every event check. The map shows the scores for inspection."
		}
	})
	return nil
}
func (s *Server) archives(ctx context.Context, j *job, a, b float64) ([][]string, error) {
	first := time.Unix(int64(a), 0).UTC()
	first = time.Date(first.Year(), first.Month(), first.Day(), 0, 0, 0, 0, time.UTC)
	end := time.Unix(int64(b), 0).UTC()
	var archives [][]string
	remaining := s.options.MaxDownloadBytes
	for day := first; !day.After(end); day = day.AddDate(0, 0, 1) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		date := day.Format("2006-01-02")
		j.phase("archive", "Finding historical flight data for "+date)
		dir := filepath.Join(s.options.DataDir, "cache", date, "prod-0")
		manifest := filepath.Join(dir, "manifest.json")
		var listed []tracks.Asset
		if raw, err := os.ReadFile(manifest); err != nil || json.Unmarshal(raw, &listed) != nil || len(listed) == 0 {
			var err error
			listed, err = tracks.ListArchive(ctx, date, "prod-0")
			if err != nil {
				return nil, fmt.Errorf("Historical flight data for %s is unavailable: %w. Try an available date or upload a flight-track CSV.", date, err)
			}
			if err = os.MkdirAll(dir, 0700); err != nil {
				return nil, err
			}
			if err = search.JSON(manifest, listed); err != nil {
				return nil, err
			}
		}
		sort.Slice(listed, func(i, k int) bool { return listed[i].Name < listed[k].Name })
		var missing []tracks.Asset
		var paths []string
		var needed int64
		for _, asset := range listed {
			if filepath.Base(asset.Name) != asset.Name {
				return nil, fmt.Errorf("invalid cached archive manifest")
			}
			path := filepath.Join(dir, asset.Name)
			paths = append(paths, path)
			info, err := os.Stat(path)
			if err == nil && info.Size() == asset.Size {
				continue
			}
			if err == nil {
				return nil, fmt.Errorf("Cached archive part has an unexpected size: %s", path)
			}
			if !os.IsNotExist(err) {
				return nil, err
			}
			if asset.Size < 0 || asset.Size > remaining-needed {
				return nil, fmt.Errorf("Flight downloads exceed the %.1f GB limit. Use a local dataset or increase --max-download-gb.", float64(s.options.MaxDownloadBytes)/1e9)
			}
			needed += asset.Size
			missing = append(missing, asset)
		}
		if len(missing) > 0 {
			j.phase("downloading", "Downloading flight history for "+date)
			err := tracks.FetchArchiveProgress(ctx, missing, dir, remaining, func(done, total int64) {
				j.update(func(i *JobInfo) {
					i.Message = fmt.Sprintf("Downloading %s: %.2f / %.2f GB", date, float64(done)/1e9, float64(total)/1e9)
					i.Completed = done
					i.Total = total
				})
			})
			if err != nil {
				return nil, err
			}
			remaining -= needed
		}
		archives = append(archives, paths)
	}
	return archives, nil
}
func (s *Server) demo(w http.ResponseWriter, r *http.Request) {
	j, err := s.newJob("Synthetic sample recording.wav")
	if err != nil {
		problem(w, 500, err.Error())
		return
	}
	j.update(func(i *JobInfo) { i.Synthetic = true })
	s.start(j, "analyzing", func(ctx context.Context) error {
		j.phase("sample", "Creating a sample with two aircraft and a known location")
		c, err := demo.Generate(filepath.Join(j.dir, "sample"))
		if err != nil {
			return err
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		if err = os.Rename(filepath.Join(j.dir, "sample", "audio.wav"), filepath.Join(j.dir, "audio")); err != nil {
			return err
		}
		if err = os.Rename(filepath.Join(j.dir, "sample", "tracks.csv"), filepath.Join(j.dir, "tracks.csv")); err != nil {
			return err
		}
		for i := range c.Events {
			c.Events[i].CPATolerance = 5
		}
		r := SearchRequest{MatchMode: "joint", RecordingStart: c.RecordingStart, Bounds: c.Bounds, Height: c.ObserverHeight, ClockUncertainty: c.ClockUncertainty, Source: "upload", Grid: 200, Refine: c.Refine, Events: c.Events}
		j.update(func(i *JobInfo) { i.HasTracks = true; i.Params = &r })
		if err = s.analyze(ctx, j, c.Events); err != nil {
			return err
		}
		spectrum, err := acoustic.Load(filepath.Join(j.dir, "spectrogram.bin"))
		if err != nil {
			return err
		}
		c, err = s.configuration(j, r, spectrum)
		if err != nil {
			return err
		}
		j.update(func(i *JobInfo) { i.State = "searching" })
		return s.find(ctx, j, r, c, spectrum)
	})
	reply(w, 202, j.snapshot())
}
