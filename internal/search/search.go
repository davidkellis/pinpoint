package search

import (
	"context"
	"fmt"
	"math"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"pinpoint/internal/acoustic"
	"pinpoint/internal/geo"
	"pinpoint/internal/tracks"
)

type Config struct {
	ObservationFold     string   `json:"observation_fold,omitempty"`
	ScoreObjective      string   `json:"score_objective,omitempty"`
	DistinctAircraft    bool     `json:"distinct_aircraft,omitempty"`
	EventSpectra        []string `json:"event_spectra,omitempty"`
	eventSpectra        []*acoustic.Spectrum
	validationChecks    []SequenceCheck
	validationCandidate *Candidate
	CurveEnvelope       bool `json:"curve_envelope_filter,omitempty"`
	clockOffsets        []float64
	eventTrackIDs       []string
	FinalGrid           float64          `json:"final_grid_m,omitempty"`
	ExcludeJets         bool             `json:"exclude_known_jets,omitempty"`
	Staged              bool             `json:"staged_joint_search,omitempty"`
	DenseGrid           float64          `json:"dense_grid_m,omitempty"`
	Silence             []SilentInterval `json:"silence_intervals,omitempty"`
	RegionMask          string           `json:"region_mask,omitempty"`
	Corridors           bool             `json:"flight_corridors,omitempty"`
	GeoidGrid           string           `json:"geoid_grid,omitempty"`
	Spectrum            string           `json:"spectrogram"`
	Tracks              string           `json:"tracks"`
	RecordingStart      string           `json:"recording_start"`
	ClockUncertainty    float64          `json:"clock_uncertainty_s"`
	ClockStep           float64          `json:"clock_step_s"`
	Bounds              geo.Bounds       `json:"bounds"`
	Grid                float64          `json:"grid_m"`
	Refine              float64          `json:"refine_m"`
	ObserverHeight      *float64         `json:"observer_height_ellipsoid_m,omitempty"`
	TerrainDir          string           `json:"terrain_dir,omitempty"`
	GeoidOffset         *float64         `json:"terrain_geoid_offset_m,omitempty"`
	MicHeight           float64          `json:"microphone_height_agl_m"`
	SoundSpeed          float64          `json:"sound_speed_mps"`
	MaxGap              float64          `json:"max_track_gap_s"`
	MinScore            float64          `json:"min_event_score_db"`
	Workers             int              `json:"workers"`
	MaxGridPoints       int              `json:"max_grid_points,omitempty"`
	OmitUnmodeled       bool             `json:"omit_unmodeled_points,omitempty"`
	Events              []acoustic.Event `json:"events"`
}

func DefaultConfig() Config {
	return Config{ClockUncertainty: 30, ClockStep: 1, Grid: 200, Refine: 40, SoundSpeed: 343, MaxGap: 30, MinScore: 5, MicHeight: 1.5, Workers: min(32, runtime.GOMAXPROCS(0))}
}

func (c Config) Validate(s *acoustic.Spectrum) (float64, error) {
	if c.ObservationFold != "" && c.ObservationFold != "train" && c.ObservationFold != "validation" {
		return 0, fmt.Errorf("unknown observation fold")
	}
	if c.ScoreObjective != "" && c.ScoreObjective != "weakest" {
		return 0, fmt.Errorf("unknown score objective")
	}
	t, err := time.Parse(time.RFC3339Nano, c.RecordingStart)
	if err != nil {
		return 0, fmt.Errorf("recording_start must be RFC3339 with a time zone: %w", err)
	}
	for _, v := range []float64{c.ClockUncertainty, c.ClockStep, c.Grid, c.Refine, c.FinalGrid, c.SoundSpeed, c.MaxGap, c.MinScore, c.MicHeight} {
		if !geo.Finite(v) {
			return 0, fmt.Errorf("nonfinite configuration")
		}
	}
	if !c.Bounds.Valid() || c.Grid < 5 || c.Refine < 0 || c.Refine > c.Grid || c.SoundSpeed < 300 || c.SoundSpeed > 370 || c.MaxGap <= 0 || c.MaxGap > 120 || c.MinScore <= 0 || c.Workers < 1 || c.Workers > 256 || c.ClockStep <= 0 || c.ClockUncertainty < 0 || 2*c.ClockUncertainty/c.ClockStep > 2000 || len(c.Events) == 0 {
		return 0, fmt.Errorf("invalid bounds/search limits; use a bounded region, at least one event and at most 2001 clock samples")
	}
	if c.FinalGrid != 0 && (c.FinalGrid < 5 || c.FinalGrid > c.Refine || c.Refine == 0) {
		return 0, fmt.Errorf("final_grid_m must be 5 metres or greater and no coarser than refine_m")
	}
	if c.MaxGridPoints < 0 || c.MaxGridPoints > 2000000 {
		return 0, fmt.Errorf("max_grid_points must be at most 2000000 (zero uses 250000)")
	}
	if c.ObserverHeight != nil {
		if !geo.Finite(*c.ObserverHeight) || *c.ObserverHeight < -1000 || *c.ObserverHeight > 10000 || c.TerrainDir != "" {
			return 0, fmt.Errorf("specify either a valid observer ellipsoidal height or terrain")
		}
	} else if c.TerrainDir == "" || (c.GeoidGrid == "" && c.GeoidOffset == nil) || (c.GeoidOffset != nil && (!geo.Finite(*c.GeoidOffset) || math.Abs(*c.GeoidOffset) > 150)) || c.MicHeight < 0 || c.MicHeight > 1000 {
		return 0, fmt.Errorf("supply observer_height_ellipsoid_m, or terrain_dir + terrain_geoid_offset_m + microphone_height_agl_m")
	}
	for i, e := range c.Events {
		for _, v := range []float64{e.Start, e.End, e.CPATime, e.CPATolerance, e.FMin, e.FMax, e.FStep, e.MinSpeed, e.MaxSpeed, e.MinSlant, e.MaxSlant} {
			if !geo.Finite(v) {
				return 0, fmt.Errorf("nonfinite event %d", i)
			}
		}
		if e.CPATolerance < 0 || e.CPATolerance > 120 || e.Partial && e.CPATolerance > 0 {
			return 0, fmt.Errorf("event %d: CPA uncertainty must be 0–120 seconds and is unavailable for partial flyovers", i+1)
		}
		if e.Start < s.Time(0) || e.End > s.Time(len(s.Rows)-1) || e.End-e.Start < 5 || e.CPATime < e.Start || e.CPATime > e.End || e.FMin <= 0 || e.FMax < e.FMin || e.FStep <= 0 || (len(e.Ridge) == 0 && (e.FMax-e.FMin)/e.FStep > 1000) || e.MinSpeed < 0 || e.MaxSpeed <= e.MinSpeed || e.MaxSpeed >= c.SoundSpeed || e.MinSlant < 0 || e.MaxSlant <= e.MinSlant || e.MaxSlant > 50000 || len(e.Harmonics) == 0 {
			return 0, fmt.Errorf("invalid event %d (time window, frequency sweep, harmonics, speed or distance)", i)
		}
		for k, p := range e.Ridge {
			if !geo.Finite(p.Time) || !geo.Finite(p.Hz) || p.Hz <= 0 || p.Time < e.Start || p.Time > e.End || k > 0 && p.Time <= e.Ridge[k-1].Time {
				return 0, fmt.Errorf("invalid observed ridge in event %d", i)
			}
		}
		seen := map[int]bool{}
		for _, h := range e.Harmonics {
			if h < 1 || h > 16 || seen[h] {
				return 0, fmt.Errorf("invalid/duplicate harmonic in event %d", i)
			}
			seen[h] = true
		}
		if i > 0 && e.Start < c.Events[i-1].Start {
			return 0, fmt.Errorf("events must be ordered by start time")
		}
	}
	if err := validateStaged(c, s); err != nil {
		return 0, err
	}
	return float64(t.Unix()) + float64(t.Nanosecond())/1e9, nil
}

type Match struct {
	Track    string          `json:"track_id"`
	F0       float64         `json:"f0_hz"`
	Score    float64         `json:"score_db"`
	Coverage float64         `json:"coverage"`
	CPA      tracks.Approach `json:"cpa"`
}
type Candidate struct {
	Lat         float64 `json:"lat"`
	Lon         float64 `json:"lon"`
	Height      float64 `json:"height_ellipsoid_m"`
	ClockOffset float64 `json:"clock_offset_s"`
	Score       float64 `json:"score_db"`
	Events      []Match `json:"events"`
}
type RefinementPass struct {
	Spacing   float64 `json:"spacing_m"`
	Locations int     `json:"locations"`
	Seeds     int     `json:"seeds"`
}
type Report struct {
	Rejections        map[string]int          `json:"rejection_counts,omitempty"`
	FinalGrid         float64                 `json:"final_grid_m,omitempty"`
	RefinementPasses  []RefinementPass        `json:"refinement_passes,omitempty"`
	TypeFilter        *tracks.TypeFilterStats `json:"aircraft_type_filter,omitempty"`
	GeometryEvaluated int                     `json:"geometry_locations_evaluated,omitempty"`
	GeometrySurvived  int                     `json:"geometry_locations_survived,omitempty"`
	DenseGrid         float64                 `json:"dense_grid_m,omitempty"`
	Ambiguity         string                  `json:"ambiguity,omitempty"`
	Status            string                  `json:"status"`
	Note              string                  `json:"note"`
	Grid              float64                 `json:"coarse_grid_m"`
	Refine            float64                 `json:"refine_grid_m"`
	Evaluated         int                     `json:"locations_evaluated"`
	Candidates        []Candidate             `json:"candidates"`
	FlightGroups      []Candidate             `json:"best_per_flight_group"`
	Hypotheses        []Candidate             `json:"followup_hypotheses,omitempty"`
	Bounds            geo.Bounds              `json:"bounds"`
	MinScore          float64                 `json:"min_event_score_db"`
	ScoreGrid         []ScorePoint            `json:"score_grid"`
}

// ScorePoint records the best passing fit at a location, or, if none passes,
// the best complete fit below the threshold. Nil score means that no complete
// model had sufficient geometry/spectral coverage, not a score of zero.
type ScorePoint struct {
	Reason         string    `json:"rejection_reason,omitempty"`
	Lat            float64   `json:"lat"`
	Lon            float64   `json:"lon"`
	Score          *float64  `json:"score_db"`
	EventScores    []float64 `json:"event_scores_db"`
	TrackIDs       []string  `json:"track_ids"`
	ClockOffset    *float64  `json:"clock_offset_s"`
	MeetsThreshold bool      `json:"meets_threshold"`
	Stage          string    `json:"stage"`
	Spacing        float64   `json:"grid_m"`
}
type observer struct {
	lat, lon, height float64
	pos              geo.Vec
	clockMask        uint64
}

func Run(ctx context.Context, c Config, s *acoustic.Spectrum, ts []tracks.Track) (Report, error) {
	return RunWithProgress(ctx, c, s, ts, nil)
}

// RunWithProgress reports finished locations on the coordinator goroutine.
func RunWithProgress(ctx context.Context, c Config, s *acoustic.Spectrum, ts []tracks.Track, progress func(stage string, completed, total int)) (Report, error) {
	return RunWithPool(ctx, c, s, ts, nil, progress)
}

// RunWithPool shares a scoring budget with other independent searches. Progress
// still runs on this search's coordinator; inputs are read-only across searches.
func RunWithPool(ctx context.Context, c Config, s *acoustic.Spectrum, ts []tracks.Track, pool *WorkerPool, progress func(stage string, completed, total int)) (Report, error) {
	if err := loadEventSpectra(&c); err != nil {
		return Report{}, err
	}
	start, err := c.Validate(s)
	if err != nil {
		return Report{}, err
	}
	var typeStats *tracks.TypeFilterStats
	if c.Staged {
		ts = TracksForEvents(c, ts)
	}
	if c.ExcludeJets {
		var stats tracks.TypeFilterStats
		ts, stats = tracks.ExcludeKnownJets(ts)
		typeStats = &stats
	}
	prepared := make([]tracks.Track, len(ts))
	for i := range ts {
		prepared[i] = ts[i].PrepareSegments()
	}
	ts = prepared
	// A segment is contained by its endpoints' ECEF bounding box. A point
	// farther than every event's maximum slant range cannot explain any event.
	maxSlant := 0.0
	for _, event := range c.Events {
		maxSlant = max(maxSlant, event.MaxSlant)
	}
	for _, interval := range c.Silence {
		maxSlant = max(maxSlant, interval.MaxSlant)
	}
	observations := make([][]scoreObservation, len(c.Events))
	for i, e := range c.Events {
		observations[i] = prepareFoldObservations(eventSpectrum(c, s, i), e, c.ObservationFold)
	}
	index := newTrackIndex(ts, maxSlant)
	terrain := geo.Terrain{Dir: c.TerrainDir}
	mask, err := geo.NewMask(c.RegionMask)
	if err != nil {
		return Report{}, err
	}
	var geoid *geo.Geoid
	if c.GeoidGrid != "" {
		geoid, err = geo.LoadGeoid(c.GeoidGrid)
		if err != nil {
			return Report{}, err
		}
	}
	makeObserver := func(lat, lon float64) (observer, error) {
		var h float64
		if c.ObserverHeight != nil {
			h = *c.ObserverHeight
		} else {
			v, e := terrain.Height(lat, lon)
			if e != nil {
				return observer{}, e
			}
			offset := 0.0
			if geoid != nil {
				offset, e = geoid.Height(lat, lon)
				if e != nil {
					return observer{}, e
				}
			} else {
				offset = *c.GeoidOffset
			}
			h = v + offset + c.MicHeight
		}
		return observer{lat: lat, lon: lon, height: h, pos: geo.ECEF(lat, lon, h)}, nil
	}
	locations, err := GridLocations(ctx, c, ts)
	if err != nil {
		return Report{}, err
	}
	var points []observer
	for _, ll := range locations {
		if err := ctx.Err(); err != nil {
			return Report{}, err
		}
		p, e := makeObserver(ll[0], ll[1])
		if e != nil {
			return Report{}, e
		}
		points = append(points, p)
	}
	geometryEvaluated, geometrySurvived := 0, 0
	scoringGrid, scoringStage := c.Grid, "coarse"
	if c.Staged {
		geometryEvaluated = len(points)
		var cells []stagedCell
		cells, geometrySurvived, err = stagedLocations(ctx, c, s, points, ts, start, pool, progress, func(p observer) float64 { return observerCellRadius(p, c, &terrain, geoid) })
		if err != nil {
			return Report{}, err
		}
		points = nil
		for _, cell := range cells {
			if err := ctx.Err(); err != nil {
				return Report{}, err
			}
			p, e := makeObserver(cell.lat, cell.lon)
			if e != nil {
				return Report{}, e
			}
			p.clockMask = cell.mask
			points = append(points, p)
		}
		scoringGrid, scoringStage = c.DenseGrid, "dense"
	}
	var surface []ScorePoint
	rejectionCounts := map[string]int{}
	evaluate := func(points []observer, stage string, spacing float64, fitConfig Config) []Candidate {
		if progress != nil {
			progress(stage, 0, len(points))
		}
		jobs := make(chan observer)
		type evaluation struct {
			candidate Candidate
			point     ScorePoint
			matched   bool
		}
		results := make(chan evaluation, c.Workers)
		var wg sync.WaitGroup
		for i := 0; i < c.Workers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for p := range jobs {
					if !pool.acquire(ctx) {
						continue
					}
					nearby := index.nearby(p.pos)
					result, point, ok := evaluatePreparedPoint(ctx, p, fitConfig, s, nearby, start, observations)
					pool.release()
					point.Stage, point.Spacing = stage, spacing
					if strings.HasPrefix(stage, "refined") {
						point.Stage = "refined"
					}
					results <- evaluation{result, point, ok}
				}
			}()
		}
		go func() {
			defer close(jobs)
			for _, p := range points {
				select {
				case jobs <- p:
				case <-ctx.Done():
					return
				}
			}
		}()
		go func() { wg.Wait(); close(results) }()
		var out []Candidate
		completed := 0
		for r := range results {
			completed++
			if r.point.Reason != "" {
				rejectionCounts[r.point.Reason]++
			}
			if progress != nil {
				progress(stage, completed, len(points))
			}
			if !c.OmitUnmodeled || r.point.Score != nil {
				surface = append(surface, r.point)
			}
			if r.matched {
				out = append(out, r.candidate)
			}
		}
		return out
	}
	out := evaluate(points, scoringStage, scoringGrid, c)
	evaluated := len(points)
	sortCandidates(out)
	var refinementPasses []RefinementPass
	previous := scoringGrid
	next := c.Refine
	if c.FinalGrid > 0 && next >= previous && c.FinalGrid < previous {
		next = math.Max(c.FinalGrid, previous/2)
	}
	seenRefined := map[string]bool{}
	for next > 0 && next < previous && len(out) > 0 {
		seeds := refinementSeeds(out, previous, 8)
		n := int(math.Ceil(previous / next))
		if n > 100 {
			return Report{}, fmt.Errorf("refinement ratio exceeds 100")
		}
		passCount := 0
		for _, seed := range seeds {
			points = nil
			for y := -n; y <= n; y++ {
				for x := -n; x <= n; x++ {
					if err := ctx.Err(); err != nil {
						return Report{}, err
					}
					lat := seed.Lat + float64(y)*next/111320
					lon := seed.Lon + float64(x)*next/(111320*math.Cos(seed.Lat*math.Pi/180))
					if !c.Bounds.Contains(lat, lon) || !mask.Contains(lat, lon) {
						continue
					}
					key := fmt.Sprintf("%.8f,%.8f/%.4f/%s", lat, lon, next, flightKey(seed))
					if seenRefined[key] {
						continue
					}
					seenRefined[key] = true
					q, e := makeObserver(lat, lon)
					if e != nil {
						return Report{}, e
					}
					points = append(points, q)
				}
			}
			// Once a flight combination survives dense scoring, tighten source-tone
			// and common-clock sampling as well as position. Other flights remain in
			// the input so that silence exclusions are still enforced.
			fitConfig := c
			if c.FinalGrid > 0 && next <= c.FinalGrid*2.5 {
				fitConfig = refinementConfig(c, seed, previous)
			}
			out = append(out, evaluate(points, fmt.Sprintf("refined %.3g m", next), next, fitConfig)...)
			passCount += len(points)
		}
		evaluated += passCount
		refinementPasses = append(refinementPasses, RefinementPass{next, passCount, len(seeds)})
		sortCandidates(out)
		previous = next
		if c.FinalGrid == 0 || next <= c.FinalGrid {
			break
		}
		next = math.Max(c.FinalGrid, next/2)
	}

	if err := ctx.Err(); err != nil {
		return Report{}, err
	}
	sort.Slice(surface, func(i, j int) bool {
		a, b := surface[i], surface[j]
		if a.Stage != b.Stage {
			return a.Stage < b.Stage
		}
		if a.Lat != b.Lat {
			return a.Lat < b.Lat
		}
		if a.Lon != b.Lon {
			return a.Lon < b.Lon
		}
		if a.Spacing != b.Spacing {
			return a.Spacing < b.Spacing
		}
		if a.Score != nil && b.Score != nil && *a.Score != *b.Score {
			return *a.Score > *b.Score
		}
		if a.ClockOffset != nil && b.ClockOffset != nil && *a.ClockOffset != *b.ClockOffset {
			return *a.ClockOffset < *b.ClockOffset
		}
		return strings.Join(a.TrackIDs, "\x00") < strings.Join(b.TrackIDs, "\x00")
	})
	report := Report{Status: "no_match", Note: "Scores are heuristic evidence, not probabilities. Grid spacing is not location accuracy. Real-recording accuracy has not been calibrated.", Grid: c.Grid, Refine: c.Refine, Evaluated: evaluated, Candidates: []Candidate{}, FlightGroups: []Candidate{}, Bounds: c.Bounds, MinScore: c.MinScore, ScoreGrid: surface}
	report.Rejections = rejectionCounts
	if geometryEvaluated > geometrySurvived {
		report.Rejections["coarse_geometry_or_curve_envelope"] = geometryEvaluated - geometrySurvived
	}
	report.TypeFilter = typeStats
	report.RefinementPasses = refinementPasses
	if len(refinementPasses) > 0 {
		report.FinalGrid = refinementPasses[len(refinementPasses)-1].Spacing
	}
	if c.Staged {
		report.GeometryEvaluated, report.GeometrySurvived, report.DenseGrid = geometryEvaluated, geometrySurvived, c.DenseGrid
		report.Note += " Staged joint search: arrival-time geometry filtering (measured CPA where available), dense scoring across all surviving regions, one shared recording clock, and distinct flight tracks. Partial curves retain their measured sample timing without an assumed CPA."
	}
	if len(c.Silence) > 0 {
		report.Note += " Reviewed silence intervals were enforced over the full modeled audible duration using the configured audibility radii; this depends on flight coverage and assumed audibility."
	}
	if c.OmitUnmodeled {
		report.Note += " Grid locations with no complete model are omitted from the map, but included in locations_evaluated."
	}
	groups := map[string]bool{}
	positions := map[string]bool{}
	for _, p := range out {
		key := fmt.Sprintf("%.8f,%.8f", p.Lat, p.Lon)
		if !positions[key] && len(report.Candidates) < 20 {
			report.Candidates = append(report.Candidates, p)
			positions[key] = true
		}
		var ids []string
		for _, e := range p.Events {
			ids = append(ids, e.Track)
		}
		group := strings.Join(ids, "/")
		if !groups[group] && len(report.FlightGroups) < 20 {
			report.FlightGroups = append(report.FlightGroups, p)
			groups[group] = true
		}
	}
	if len(out) > 0 {
		report.Status = "candidate_matches"
	}
	// Keep a separate, spatially diverse pool before the display's top-20
	// truncation. A single-flight peak must not erase all timing alternatives.
	report.Hypotheses = followupSeeds(out, 500, 5, 256)
	AssessAmbiguity(&report)
	return report, nil
}

// This flags competing geographic hypotheses; it is not a confidence model.
func AssessAmbiguity(report *Report) {
	if len(report.FlightGroups) == 0 && len(report.Candidates) == 0 {
		return
	}
	var best Candidate
	if len(report.FlightGroups) > 0 {
		best = report.FlightGroups[0]
	} else {
		best = report.Candidates[0]
	}
	// A single trajectory can fit distant (often mirrored) positions. Grouping
	// by aircraft, and truncating the top candidates to one dense peak, hides
	// that ambiguity. Inspect the full scored surface before grouping it away.
	for _, p := range report.ScoreGrid {
		if p.Score == nil || !p.MeetsThreshold || best.Score-*p.Score > 1 {
			continue
		}
		if geo.ECEF(best.Lat, best.Lon, 0).Sub(geo.ECEF(p.Lat, p.Lon, 0)).Norm() > 10000 {
			report.Ambiguity = "Ambiguous: locations more than 10 km apart score within 1 dB of the top fit, including possible alternatives for the same aircraft. This recording does not establish one location."
			return
		}
	}
	if len(report.FlightGroups) == 0 {
		return
	}
	for _, other := range report.FlightGroups[1:] {
		if best.Score-other.Score > 1 {
			break
		}
		distance := geo.ECEF(best.Lat, best.Lon, best.Height).Sub(geo.ECEF(other.Lat, other.Lon, other.Height)).Norm()
		if distance > 10000 {
			report.Ambiguity = "Ambiguous: different aircraft and locations more than 10 km apart score within 1 dB of the top fit. This recording does not establish one location."
			return
		}
	}
}
func sortCandidates(out []Candidate) {
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Score != b.Score {
			return a.Score > b.Score
		}
		if a.Lat != b.Lat {
			return a.Lat < b.Lat
		}
		if a.Lon != b.Lon {
			return a.Lon < b.Lon
		}
		if a.ClockOffset != b.ClockOffset {
			return a.ClockOffset < b.ClockOffset
		}
		return flightKey(a) < flightKey(b)
	})
}

func evaluatePoint(ctx context.Context, p observer, c Config, s *acoustic.Spectrum, ts []tracks.Track, start float64) (Candidate, ScorePoint, bool) {
	observations := make([][]scoreObservation, len(c.Events))
	for i, e := range c.Events {
		observations[i] = prepareFoldObservations(eventSpectrum(c, s, i), e, c.ObservationFold)
	}
	return evaluatePreparedPoint(ctx, p, c, s, ts, start, observations)
}
func evaluatePreparedPoint(ctx context.Context, p observer, c Config, s *acoustic.Spectrum, ts []tracks.Track, start float64, observations [][]scoreObservation) (Candidate, ScorePoint, bool) {

	point := ScorePoint{Lat: p.lat, Lon: p.lon, EventScores: []float64{}, TrackIDs: []string{}, Reason: "no_eligible_trajectory"}
	approaches := make([]tracks.Approach, len(ts))
	eligible := make([][]int, len(c.Events))
	for i, t := range ts {
		a, valid := t.CPA(p.pos, c.SoundSpeed, c.MaxGap)
		approaches[i] = a
		if !valid {
			continue
		}
		for j, event := range c.Events {
			if len(c.eventTrackIDs) > 0 && t.ID != c.eventTrackIDs[j] {
				continue
			}
			if event.Partial {
				bounded, ok := t.ApproachWithin(p.pos, start-c.ClockUncertainty+event.Start, start+c.ClockUncertainty+event.End, c.SoundSpeed, c.MaxGap)
				if ok && bounded.Distance >= event.MinSlant && bounded.Distance <= event.MaxSlant && bounded.Speed >= event.MinSpeed && bounded.Speed <= event.MaxSpeed {
					eligible[j] = append(eligible[j], i)
				}
				continue
			}
			if a.Distance >= event.MinSlant && a.Distance <= event.MaxSlant && a.Speed >= event.MinSpeed && a.Speed <= event.MaxSpeed && eventPossible(event, a, start, c.ClockUncertainty) {
				eligible[j] = append(eligible[j], i)
			}
		}
	}
	for _, indices := range eligible {
		if len(indices) == 0 {
			return Candidate{}, point, false
		}
	}
	best := Candidate{Score: math.Inf(-1)}
	bestComplete := Candidate{Score: math.Inf(-1)}
	preparedTracks := make([]tracks.Track, len(ts))
	sawClock, sawSilenceAllowed, sawTiming, sawAssignmentConflict := false, false, false, false
	exclusions := silenceExclusions(c, ts, p)
	offsets := c.clockOffsets
	if len(offsets) == 0 {
		steps := int(math.Ceil(2 * c.ClockUncertainty / c.ClockStep))
		for tick := 0; tick <= steps; tick++ {
			offsets = append(offsets, min(-c.ClockUncertainty+float64(tick)*c.ClockStep, c.ClockUncertainty))
		}
	}
	for _, offset := range offsets {
		if ctx.Err() != nil {
			return Candidate{}, point, false
		}
		if !clockMaskAllows(p.clockMask, c, offset) {
			continue
		}
		sawClock = true
		recordStart := start + offset
		if !silenceClockAllows(exclusions, recordStart) {
			continue
		}
		sawSilenceAllowed = true
		// Check all event timing constraints before expensive spectral scoring.
		possible := true
		for j, event := range c.Events {
			found := false
			for _, i := range eligible[j] {
				if eventAtClock(event, approaches[i], recordStart) {
					found = true
					break
				}
			}
			if !found {
				possible = false
				break
			}
		}
		if !possible {
			continue
		}
		sawTiming = true
		matches := make([]Match, 0, len(c.Events))
		total := 0.0
		passes := true
		var choices [][]Match
		for j, event := range c.Events {
			match := Match{Score: math.Inf(-1)}
			var options []Match
			for _, i := range eligible[j] {
				if preparedTracks[i].Points == nil {
					preparedTracks[i] = ts[i].ForObserver(p.pos, c.SoundSpeed)
				}
				t := preparedTracks[i]
				a := approaches[i]
				if !eventAtClock(event, a, recordStart) {
					continue
				}
				f, score, coverage := scoreObservedTrack(t, p.pos, eventSpectrum(c, s, j), event, recordStart, c.SoundSpeed, c.MaxGap, observations[j])
				if len(c.Events) > 1 && geo.Finite(score) {
					options = append(options, Match{t.ID, f, score, coverage, a})
				}
				if score > match.Score {
					match = Match{t.ID, f, score, coverage, a}
				}
			}
			if !geo.Finite(match.Score) {
				break
			}
			if match.Score < c.MinScore {
				passes = false
			}
			matches = append(matches, match)
			total += match.Score
			if len(c.Events) > 1 {
				choices = append(choices, options)
			}
		}
		if len(c.Events) > 1 && len(choices) == len(c.Events) {
			matches = configuredMatches(c, choices, c.MinScore)
			if len(matches) != len(c.Events) && passes {
				sawAssignmentConflict = true
			}
			passes = len(matches) == len(c.Events)
			if !passes {
				matches = configuredMatches(c, choices, math.Inf(-1))
			}
			total = 0
			for _, m := range matches {
				total += m.Score
			}
		}
		if len(matches) == len(c.Events) {
			utility := total / float64(len(matches))
			if c.ScoreObjective == "weakest" {
				utility = math.Inf(1)
				for _, m := range matches {
					utility = math.Min(utility, m.Score)
				}
			}
			candidate := Candidate{p.lat, p.lon, p.height, offset, utility, matches}
			if candidate.Score > bestComplete.Score {
				bestComplete = candidate
			}
			if passes && candidate.Score > best.Score {
				best = candidate
			}
		}
	}
	matched := geo.Finite(best.Score)
	point.Reason = "curve_shape_or_coverage"
	if !sawClock {
		point.Reason = "clock_envelope"
	} else if !sawSilenceAllowed {
		point.Reason = "reviewed_silence"
	} else if !sawTiming {
		point.Reason = "relative_arrival_timing"
	} else if sawAssignmentConflict && !geo.Finite(bestComplete.Score) {
		point.Reason = "aircraft_identity_conflict"
	}
	selected := bestComplete
	if matched {
		selected = best
		point.Reason = ""
	}
	if !matched && geo.Finite(selected.Score) {
		point.Reason = "weak_spectral_signal"
	}
	if geo.Finite(selected.Score) {
		point.Score, point.ClockOffset = &selected.Score, &selected.ClockOffset
		point.MeetsThreshold = matched
		for _, event := range selected.Events {
			point.EventScores = append(point.EventScores, event.Score)
			point.TrackIDs = append(point.TrackIDs, event.Track)
		}
	}
	return best, point, matched
}

type scoreObservation struct {
	frame    int
	time, hz float64
	hasRidge bool
}

func prepareObservations(s *acoustic.Spectrum, e acoustic.Event) []scoreObservation {
	stride := max(1, int(math.Round(1/s.Step())))
	first := max(0, int(math.Ceil((e.Start-s.Time(0))/s.Step())))
	last := min(len(s.Rows)-1, int(math.Floor((e.End-s.Time(0))/s.Step())))
	out := make([]scoreObservation, 0, max(0, (last-first)/stride+1))
	for frame := first; frame <= last; frame += stride {
		t := s.Time(frame)
		hz, ok := e.RidgeHz(t)
		out = append(out, scoreObservation{frame, t, hz, ok})
	}
	return out
}
func scoreTrack(t tracks.Track, p geo.Vec, s *acoustic.Spectrum, e acoustic.Event, start, c, gap float64) (float64, float64, float64) {
	return scoreObservedTrack(t, p, s, e, start, c, gap, prepareObservations(s, e))
}
func scoreObservedTrack(t tracks.Track, p geo.Vec, s *acoustic.Spectrum, e acoustic.Event, start, c, gap float64, observations []scoreObservation) (float64, float64, float64) {
	return scoreObservedTrackDiagnostic(t, p, s, e, start, c, gap, observations, nil)
}

func scoreObservedTrackDiagnostic(t tracks.Track, p geo.Vec, s *acoustic.Spectrum, e acoustic.Event, start, c, gap float64, observations []scoreObservation, diagnostic *FitDiagnostic) (float64, float64, float64) {
	if diagnostic == nil {
		lo, hi, ok := t.ArrivalSpan(p, c)
		if !ok {
			return 0, math.Inf(-1), 0
		}
		a := sort.Search(len(observations), func(i int) bool { return start+observations[i].time > lo })
		b := sort.Search(len(observations), func(i int) bool { return start+observations[i].time > hi })
		if float64(b-a) < .95*float64(len(observations)) {
			return 0, math.Inf(-1), 0
		}
	}
	// A subset's least-squares error cannot exceed the full curve's minimum
	// squared error. Reject impossible fits cheaply; diagnostics still calculate
	// all residuals, and every survivor goes through the unchanged full scorer.
	if diagnostic == nil && !ridgeSubsetPossible(t, p, e, start, c, gap, observations) {
		return 0, math.Inf(-1), 0
	}
	type sample struct {
		frame      int
		factor, hz float64
		hasRidge   bool
	}
	// Automatic windows contain at most 121 one-Hz samples. Longer manually
	// selected windows grow the slice normally, without truncating evidence.
	var storage [128]sample
	samples := storage[:0]
	minDistance := math.Inf(1)
	expected := len(observations)
	for _, observation := range observations {
		factor, distance, ok := t.Received(p, start+observation.time, c, gap)
		if ok {
			samples = append(samples, sample{observation.frame, factor, observation.hz, observation.hasRidge})
			minDistance = math.Min(minDistance, distance)
		}
	}
	if diagnostic != nil {
		diagnostic.ExpectedSamples = expected
		diagnostic.GeometrySamples = len(samples)
		diagnostic.Reason = "frequency_bounds"
	}

	if expected < 5 || float64(len(samples))/float64(expected) < 0.95 || e.Partial && (minDistance < e.MinSlant || minDistance > e.MaxSlant) {
		if diagnostic != nil {
			diagnostic.Reason = "geometry_coverage_or_distance"
		}
		return 0, math.Inf(-1), 0
	}
	best, f0, coverage := math.Inf(-1), 0.0, 0.0
	var frequencyStorage [5]float64
	frequencies := frequencyStorage[:0]
	if len(e.Ridge) > 0 {
		xy, xx := 0.0, 0.0
		for _, q := range samples {
			if q.hasRidge {
				xy += q.factor * q.hz
				xx += q.factor * q.factor
			}
		}
		if xx == 0 {
			if diagnostic != nil {
				diagnostic.Reason = "no_measured_ridge_overlap"
			}
			return 0, math.Inf(-1), 0
		}
		center := xy / xx
		for k := -2; k <= 2; k++ {
			// Include the constrained optimum at either bound. In particular,
			// floating-point roundoff must not empty a fixed-F0 search.
			f := max(e.FMin, min(e.FMax, center+float64(k)*e.FStep))
			if len(frequencies) == 0 || f != frequencies[len(frequencies)-1] {
				frequencies = append(frequencies, f)
			}
		}
	} else {
		n := int(math.Ceil((e.FMax - e.FMin) / e.FStep))
		for k := 0; k <= n; k++ {
			frequencies = append(frequencies, min(e.FMin+float64(k)*e.FStep, e.FMax))
		}
	}
	for _, f := range frequencies {
		if len(e.Ridge) > 0 {
			squared := 0.0
			count := 0
			scale := 0.0
			for _, q := range samples {
				if q.hasRidge {
					delta := f*q.factor - q.hz
					squared += delta * delta
					scale += q.hz
					count++
				}
			}
			limit := math.Max(1.5, scale/math.Max(1, float64(count))*.012)
			if !e.Partial && e.Measurement != nil && e.Measurement.Usable {
				limit = math.Min(limit, e.Measurement.LinearRMSE*.9)
			}
			rmse := math.Sqrt(squared / math.Max(1, float64(count)))
			if diagnostic != nil && (diagnostic.RMSE == nil || rmse < *diagnostic.RMSE) {
				measured, threshold := rmse, limit
				diagnostic.RMSE, diagnostic.RMSELimit = &measured, &threshold
				diagnostic.RidgeSamples, diagnostic.F0 = count, f
				diagnostic.Reason = "ridge_residual"
				if count < max(5, int(float64(expected)*.7)) {
					diagnostic.Reason = "ridge_coverage"
				}
			}
			if count < max(5, int(float64(expected)*.7)) || rmse > limit {
				continue
			}
		}
		sum := 0.0
		count := 0
		for _, q := range samples {
			for _, h := range e.Harmonics {
				v, ok := s.Value(q.frame, f*q.factor*float64(h))
				if ok {
					sum += max(-10.0, min(30.0, v))
					count++
				}
			}
		}
		cov := float64(count) / float64(expected*len(e.Harmonics))
		if cov < 0.95 {
			if diagnostic != nil {
				diagnostic.Reason = "spectral_band_coverage"
			}
			continue
		}
		score := sum / float64(count)
		if score > best {
			best, f0, coverage = score, f, cov
			if diagnostic != nil {
				value := score
				diagnostic.Reason, diagnostic.F0, diagnostic.Score = "scored", f, &value
			}
		}
	}
	if diagnostic != nil && geo.Finite(best) {
		diagnostic.Reason, diagnostic.F0 = "scored", f0
		if len(e.Ridge) > 0 {
			squared, n := 0.0, 0
			for _, q := range samples {
				if q.hasRidge {
					delta := f0*q.factor - q.hz
					squared += delta * delta
					n++
				}
			}
			rmse := math.Sqrt(squared / math.Max(1, float64(n)))
			diagnostic.RMSE = &rmse
		}
	}
	return f0, best, coverage
}

func ridgeSubsetPossible(t tracks.Track, p geo.Vec, e acoustic.Event, start, sound, gap float64, observations []scoreObservation) bool {
	if len(e.Ridge) == 0 || len(observations) < 24 {
		return true
	}
	n, maxHz := 0, 0.0
	for _, o := range observations {
		if o.hasRidge {
			n++
			maxHz = max(maxHz, o.hz)
		}
	}
	limit := max(1.5, maxHz*.012)
	if !e.Partial && e.Measurement != nil && e.Measurement.Usable {
		limit = min(limit, e.Measurement.LinearRMSE*.9)
	}
	xy, xx, yy := 0.0, 0.0, 0.0
	for _, eighth := range []int{0, 8, 4, 2, 6, 1, 3, 5, 7} {
		o := observations[eighth*(len(observations)-1)/8]
		if !o.hasRidge {
			continue
		}
		factor, _, ok := t.Received(p, start+o.time, sound, gap)
		if !ok {
			continue
		}
		xy += factor * o.hz
		xx += factor * factor
		yy += o.hz * o.hz
		f := max(e.FMin, min(e.FMax, xy/xx))
		error := yy - 2*f*xy + f*f*xx
		if error > float64(n)*limit*limit+1e-8*max(1, yy) {
			return false
		}
	}
	return true
}
