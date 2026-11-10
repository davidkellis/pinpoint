package search

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"pinpoint/internal/acoustic"
	"pinpoint/internal/geo"
	"pinpoint/internal/tracks"
)

// A single Doppler curve constrains roughly four quantities (source pitch,
// aircraft speed, closest-approach time and distance). Across a whole state many
// airborne aircraft reproduce any one curve from some receiver position beside
// their track, so single-curve searches produce near-tied, widely separated
// winners. Consensus search instead looks for receiver positions where several
// independently detected curves are explained by distinct aircraft under one
// shared recording clock. Curves that no aircraft explains (sounds without
// telemetry, or not aircraft at all) are left unassigned instead of being forced.

// RidgeSubsetReview is the best group of at least Minimum observed curves that
// distinct aircraft explain at one receiver and clock.
type RidgeSubsetReview struct {
	Matches []JointRidgeMatch `json:"matches"`
	// Worst unexplained curvature (see Unexplained) of the best Minimum-sized
	// group. It is the ranking metric, so positions remain comparable when more
	// curves fit.
	Worst float64 `json:"worst_unexplained_curvature"`
	// Curves explained in total after greedily adding further compatible curves.
	Explained    int  `json:"curves_explained"`
	Valid        bool `json:"valid"`
	ReservedPass bool `json:"all_reserved_ridge_checks_pass"`
}

func eventsOverlap(a, b acoustic.Event) bool { return a.Start < b.End && b.Start < a.End }

// Unexplained is the fraction of a curve's bend that an aircraft fit leaves
// unexplained: the Doppler training residual divided by the residual of a
// straight line through the same samples. Raw residuals in Hz cannot separate
// informative curves from nearly flat or nearly straight ones, which almost any
// aircraft "explains" somewhere; this ratio is scale free and only small when
// the specific geometry reproduces the curvature.
func Unexplained(f CurveModelFit) float64 { return f.Training / math.Max(f.TrainLinear, 1e-9) }

// SubsetCriteria decides which single-curve fits count as explanations.
type SubsetCriteria struct {
	MaxNormalized  float64 `json:"max_normalized_training_rmse"`
	MaxUnexplained float64 `json:"max_unexplained_curvature"`
	MinSwingHz     float64 `json:"min_swing_hz"`
	// SeparatePasses lets one aircraft explain several curves when they lie
	// in clearly separate passes over the receiver: between them the aircraft
	// moved at least PassProminence farther away and came back.
	SeparatePasses bool `json:"separate_passes,omitempty"`
}

func DefaultSubsetCriteria() SubsetCriteria {
	return SubsetCriteria{MaxNormalized: 1, MaxUnexplained: .35, MinSwingHz: 3}
}

func (c SubsetCriteria) eligible(q JointRidgeMatch) bool {
	return q.NormalizedTraining <= c.MaxNormalized && Unexplained(q.Fit) <= c.MaxUnexplained && q.Fit.TrainConstant >= c.MinSwingHz
}

// FitRidgeSubset fits every curve at one receiver/clock and selects the group
// of `minimum` mutually nonoverlapping curves, assigned to distinct aircraft,
// with the smallest worst unexplained curvature. Only fits meeting the criteria
// count: enough Doppler swing, a bounded normalized residual, and most of the
// bend explained. Remaining compatible curves are then added greedily; they are
// reported as corroboration but do not change the ranking metric.
func FitRidgeSubset(ctx context.Context, h CurveReceiver, ts []tracks.Track, events []acoustic.Event, o CurveModelOptions, minimum int, crit SubsetCriteria) (RidgeSubsetReview, error) {
	r := RidgeSubsetReview{Matches: []JointRidgeMatch{}}
	if minimum < 1 || minimum > 6 || len(events) < minimum || len(events) > 32 {
		return r, fmt.Errorf("ridge subsets require 1..6 required curves among at most 32")
	}
	if !(crit.MaxNormalized > 0) || !(crit.MaxUnexplained > 0) || crit.MinSwingHz < 0 {
		return r, fmt.Errorf("invalid subset criteria")
	}
	for i, e := range events {
		if len(e.Ridge) < 8 || e.End <= e.Start || i > 0 && e.Start < events[i-1].Start {
			return r, fmt.Errorf("ridge subsets need start-ordered curves with measured ridge samples")
		}
	}
	start, err := time.Parse(time.RFC3339Nano, h.RecordingStart)
	if err != nil {
		return r, err
	}
	rows, err := ridgeRows(ctx, h, start, ts, events, o, crit.MaxNormalized)
	if err != nil {
		return r, err
	}
	for i := range rows {
		kept := rows[i][:0]
		for _, q := range rows[i] {
			if crit.eligible(q) {
				kept = append(kept, q)
			}
		}
		rows[i] = kept
	}
	if crit.SeparatePasses {
		epoch := float64(start.Unix()) + float64(start.Nanosecond())/1e9 + h.Clock
		assignPasses(rows, h, epoch, ts, events)
	}
	var usable []int
	for i := range rows {
		if len(rows[i]) > 0 {
			usable = append(usable, i)
		}
	}
	if len(usable) < minimum {
		return r, nil
	}
	assign := func(set []int) ([]JointRidgeMatch, float64, bool) {
		sub := make([][]JointRidgeMatch, len(set))
		for k, i := range set {
			sub[k] = append([]JointRidgeMatch(nil), rows[i]...)
		}
		m, ok := assignRidgeAircraftBy(sub, func(q JointRidgeMatch) float64 { return Unexplained(q.Fit) })
		if !ok {
			return nil, 0, false
		}
		worst := 0.
		for _, q := range m {
			worst = math.Max(worst, Unexplained(q.Fit))
		}
		return m, worst, true
	}
	best, bestSet, found := math.Inf(1), []int(nil), false
	var bestMatches []JointRidgeMatch
	set := make([]int, 0, minimum)
	var choose func(from int)
	choose = func(from int) {
		if len(set) == minimum {
			if m, worst, ok := assign(set); ok && worst < best {
				best, bestSet, bestMatches, found = worst, append([]int(nil), set...), m, true
			}
			return
		}
		for k := from; k <= len(usable)-(minimum-len(set)); k++ {
			i := usable[k]
			compatible := true
			for _, j := range set {
				if eventsOverlap(events[i], events[j]) {
					compatible = false
					break
				}
			}
			if !compatible {
				continue
			}
			set = append(set, i)
			choose(k + 1)
			set = set[:len(set)-1]
		}
	}
	choose(0)
	if !found {
		return r, nil
	}
	chosen, matches := bestSet, bestMatches
	for _, i := range usable {
		compatible := true
		for _, j := range chosen {
			if i == j || eventsOverlap(events[i], events[j]) {
				compatible = false
				break
			}
		}
		if !compatible {
			continue
		}
		trial := append(append([]int(nil), chosen...), i)
		sort.Ints(trial)
		if m, _, ok := assign(trial); ok {
			chosen, matches = trial, m
		}
	}
	r.Valid, r.Worst, r.Explained, r.Matches, r.ReservedPass = true, best, len(matches), matches, true
	for _, m := range matches {
		r.ReservedPass = r.ReservedPass && m.Fit.Shape
	}
	return r, nil
}

// ConsensusOptions configures a coarse-to-fine receiver search over curves.
type ConsensusOptions struct {
	Bounds         geo.Bounds `json:"bounds"`
	RegionMask     string     `json:"region_mask,omitempty"`
	RecordingStart string     `json:"recording_start"`
	Grid           float64    `json:"grid_m"`
	Refine         float64    `json:"refine_m"`
	Final          float64    `json:"final_grid_m"`
	RefinePeaks    int        `json:"refine_peaks"`
	FinalPeaks     int        `json:"final_peaks"`
	ClockWindow    float64    `json:"clock_window_s"`
	ClockStep      float64    `json:"clock_step_s"`
	// FineClockStep is used around each seed's clock during refinement; the
	// final stage halves it. Zero keeps ClockStep.
	FineClockStep float64        `json:"fine_clock_step_s"`
	MinCurves     int            `json:"min_curves"`
	Criteria      SubsetCriteria `json:"criteria"`
	// Separation between reported regions; the default is two miles.
	RegionSeparation float64 `json:"region_separation_m"`
	// A winner is "resolved" only when the best region at least
	// RegionSeparation away has a worst unexplained curvature at least this many times larger.
	ResolveRatio float64 `json:"resolve_ratio"`
	// Regions is the number of separated regions to report (default 10).
	Regions int `json:"regions"`
	// Loudness, when supplied, adds a close-pass loudness check to each region.
	Loudness        *acoustic.Loudness `json:"-"`
	LoudnessOptions LoudnessOptions    `json:"loudness_options"`
	// ExcludeKnownJets removes aircraft whose type code is a known jet from the
	// candidates that may explain a curve (the source was heard as a
	// propeller). Unknown types stay. Jets still count in the loudness check.
	ExcludeKnownJets bool    `json:"exclude_known_jets,omitempty"`
	MaxPoints        int     `json:"max_points"`
	Workers          int     `json:"workers"`
	Window           float64 `json:"fft_window_s"`
	MaxSourceChange  float64 `json:"max_fractional_source_change"`
	SoundSpeed       float64 `json:"sound_speed_mps"`
	MaxGap           float64 `json:"max_track_gap_s"`
	// Height returns the receiver ellipsoidal height. It is required.
	Height   func(lat, lon float64) (float64, error)           `json:"-"`
	Progress func(stage string, done, total int, best float64) `json:"-"`
}

func DefaultConsensusOptions() ConsensusOptions {
	return ConsensusOptions{Grid: 1000, Refine: 100, Final: 20, RefinePeaks: 80, FinalPeaks: 40, ClockWindow: 6, ClockStep: 2, MinCurves: 2, Criteria: DefaultSubsetCriteria(), RegionSeparation: 3218.688, ResolveRatio: 1.5, Regions: 30, LoudnessOptions: DefaultLoudnessOptions(), MaxPoints: 2000000, Workers: 4, Window: 2, SoundSpeed: 343, MaxGap: 30}
}

type ConsensusPoint struct {
	Lat      float64           `json:"lat"`
	Lon      float64           `json:"lon"`
	Height   float64           `json:"height_ellipsoid_m"`
	Spacing  float64           `json:"grid_m"`
	Clock    float64           `json:"shared_clock_correction_s"`
	Worst    float64           `json:"worst_unexplained_curvature"`
	Matches  []JointRidgeMatch `json:"matches,omitempty"`
	Explains int               `json:"curves_explained"`
	Valid    bool              `json:"valid"`
	Pass     bool              `json:"all_reserved_ridge_checks_pass"`
	// Loudness compares predicted close passes with the recording's level;
	// it is computed for reported regions only, when audio loudness is supplied.
	Loudness *LoudnessCheck `json:"loudness,omitempty"`
	// Contradicted marks regions whose predicted close passes are mostly unheard.
	Contradicted bool `json:"loudness_contradicted,omitempty"`
}

func (p ConsensusPoint) better(q ConsensusPoint) bool {
	if p.Valid != q.Valid {
		return p.Valid
	}
	if p.Worst != q.Worst {
		return p.Worst < q.Worst
	}
	if p.Lat != q.Lat {
		return p.Lat < q.Lat
	}
	return p.Lon < q.Lon
}

func (p ConsensusPoint) aircraftKey() string {
	ids := make([]string, 0, len(p.Matches))
	for _, m := range p.Matches {
		ids = append(ids, fmt.Sprintf("%d:%s", m.Event, m.owner()))
	}
	sort.Strings(ids)
	return strings.Join(ids, "/")
}

type ConsensusResult struct {
	Options ConsensusOptions `json:"options"`
	Curves  int              `json:"curves_supplied"`
	Tracks  int              `json:"track_segments_screened"`
	// TypeFilter reports the known-jet exclusion when it was requested.
	TypeFilter tracks.TypeFilterStats `json:"type_filter,omitempty"`
	// Regions are the best point of each separated area, best first.
	Regions []ConsensusPoint `json:"regions"`
	Status  string           `json:"status"`
	// RunnerUp is the runner-up region's worst unexplained curvature divided by the
	// winner's; nil when fewer than two regions exist.
	RunnerUp *float64 `json:"runner_up_ratio,omitempty"`
	// AgreementRadius is the largest distance from the winner to another
	// non-contradicted region whose residual is within ResolveRatio of it: an
	// honest spread of near-tied answers (0 when the winner stands alone).
	AgreementRadius float64          `json:"agreement_radius_m"`
	Evaluated       int              `json:"evaluated_points"`
	Elapsed         float64          `json:"elapsed_s"`
	Points          []ConsensusPoint `json:"-"`
	Note            string           `json:"note"`
}

// TrackIndex buckets immutable tracks into 25 km ECEF cells enlarged by radius,
// so receiver workers only fit aircraft that could have been in acoustic range.
func TrackIndex(ts []tracks.Track, radius float64) func(geo.Vec) []tracks.Track {
	const cell = 25000.
	index := map[[3]int][]tracks.Track{}
	for _, t := range ts {
		if len(t.Points) < 2 {
			continue
		}
		lo := geo.Vec{X: math.Inf(1), Y: math.Inf(1), Z: math.Inf(1)}
		hi := geo.Vec{X: math.Inf(-1), Y: math.Inf(-1), Z: math.Inf(-1)}
		for _, p := range t.Points {
			lo = geo.Vec{X: math.Min(lo.X, p.Position.X), Y: math.Min(lo.Y, p.Position.Y), Z: math.Min(lo.Z, p.Position.Z)}
			hi = geo.Vec{X: math.Max(hi.X, p.Position.X), Y: math.Max(hi.Y, p.Position.Y), Z: math.Max(hi.Z, p.Position.Z)}
		}
		for x := int(math.Floor((lo.X - radius) / cell)); x <= int(math.Floor((hi.X+radius)/cell)); x++ {
			for y := int(math.Floor((lo.Y - radius) / cell)); y <= int(math.Floor((hi.Y+radius)/cell)); y++ {
				for z := int(math.Floor((lo.Z - radius) / cell)); z <= int(math.Floor((hi.Z+radius)/cell)); z++ {
					key := [3]int{x, y, z}
					index[key] = append(index[key], t)
				}
			}
		}
	}
	return func(p geo.Vec) []tracks.Track {
		return index[[3]int{int(math.Floor(p.X / cell)), int(math.Floor(p.Y / cell)), int(math.Floor(p.Z / cell))}]
	}
}

// TrimTracks conservatively keeps only track segments near the bounds (25 km)
// and near any curve's time span (200 s plus the clock window). It never
// selects an aircraft identity or bridges disjoint windows.
func TrimTracks(ts []tracks.Track, b geo.Bounds, events []acoustic.Event, epoch, clock float64) []tracks.Track {
	dy := 25000. / 110000
	dx := dy / math.Cos(math.Max(math.Abs(b.South), math.Abs(b.North))*math.Pi/180)
	var out []tracks.Track
	for _, t := range ts {
		first, last := len(t.Points), -1
		for i := 0; i+1 < len(t.Points); i++ {
			a, z := t.Points[i], t.Points[i+1]
			if math.Max(a.Lat, z.Lat) < b.South-dy || math.Min(a.Lat, z.Lat) > b.North+dy || math.Max(a.Lon, z.Lon) < b.West-dx || math.Min(a.Lon, z.Lon) > b.East+dx {
				continue
			}
			for _, e := range events {
				if z.Time >= epoch+e.Start-200-clock && a.Time <= epoch+e.End+200+clock {
					first = min(first, i)
					last = max(last, i+1)
				}
			}
		}
		if last <= first {
			continue
		}
		t.Points = append([]tracks.Point(nil), t.Points[first:last+1]...)
		var motion []tracks.KinematicPoint
		for _, m := range t.Motion {
			if m.Time >= t.Points[0].Time && m.Time <= t.Points[len(t.Points)-1].Time {
				motion = append(motion, m)
			}
		}
		t.Motion = motion
		out = append(out, t)
	}
	return out
}

func (o ConsensusOptions) validate(events []acoustic.Event) error {
	for _, x := range []float64{o.Grid, o.Refine, o.Final, o.ClockWindow, o.ClockStep, o.FineClockStep, o.Criteria.MaxNormalized, o.Criteria.MaxUnexplained, o.Criteria.MinSwingHz, o.RegionSeparation, o.ResolveRatio, o.Window, o.MaxSourceChange, o.SoundSpeed, o.MaxGap} {
		if !geo.Finite(x) {
			return fmt.Errorf("nonfinite consensus setting")
		}
	}
	switch {
	case !o.Bounds.Valid():
		return fmt.Errorf("invalid consensus bounds")
	case o.Height == nil:
		return fmt.Errorf("consensus search requires receiver heights")
	case o.Grid < 10 || o.Refine < 5 || o.Refine > o.Grid || o.Final < 1 || o.Final > o.Refine:
		return fmt.Errorf("grid spacings must satisfy grid >= refine >= final")
	case o.RefinePeaks < 1 || o.RefinePeaks > 100 || o.FinalPeaks < 1 || o.FinalPeaks > 100:
		return fmt.Errorf("refine/final peaks must be 1..100")
	case o.ClockWindow < 0 || o.ClockWindow > 600 || o.ClockStep <= 0 || 2*o.ClockWindow/o.ClockStep > 400 || o.FineClockStep < 0 || o.FineClockStep > o.ClockStep:
		return fmt.Errorf("invalid clock window/steps")
	case o.MinCurves < 1 || o.MinCurves > 6 || len(events) < o.MinCurves || len(events) > 32:
		return fmt.Errorf("consensus requires 1..6 required curves among at most 32 supplied")
	case o.MaxPoints < 1 || o.Workers < 1 || o.Workers > 64:
		return fmt.Errorf("invalid point or worker limits")
	case o.RegionSeparation <= 0 || o.ResolveRatio < 1:
		return fmt.Errorf("invalid region separation or resolve ratio")
	}
	return nil
}

type consensusTask struct {
	lat, lon, spacing      float64
	clock, half, clockStep float64
}

// Consensus searches the bounds coarse-to-fine for receivers where at least
// MinCurves supplied curves are explained by distinct aircraft under one shared
// clock. Ranking uses training residuals only. Region status is a heuristic
// separation check, not calibrated confidence.
func Consensus(ctx context.Context, events []acoustic.Event, ts []tracks.Track, o ConsensusOptions) (ConsensusResult, error) {
	started := time.Now()
	if o.FineClockStep == 0 {
		o.FineClockStep = o.ClockStep
	}
	r := ConsensusResult{Options: o, Curves: len(events), Status: "no_match", Note: "Training-ranked consensus of independently detected curves: at least min_curves curves must be explained by distinct aircraft at one receiver and one shared clock, each explaining most of its curve's bend. Unexplained curves stay unassigned. Region status compares separated areas heuristically; it is not a calibrated probability, and grid spacing is not accuracy."}
	if err := o.validate(events); err != nil {
		return r, err
	}
	events = append([]acoustic.Event(nil), events...)
	sort.SliceStable(events, func(i, j int) bool { return events[i].Start < events[j].Start })
	t0, err := time.Parse(time.RFC3339Nano, o.RecordingStart)
	if err != nil {
		return r, fmt.Errorf("recording start requires a time zone: %w", err)
	}
	epoch := float64(t0.Unix()) + float64(t0.Nanosecond())/1e9
	ts = TrimTracks(ts, o.Bounds, events, epoch, o.ClockWindow+o.ClockStep)
	for i := range ts {
		ts[i] = ts[i].PrepareSegments()
	}
	allTracks := ts
	if o.ExcludeKnownJets {
		ts, r.TypeFilter = tracks.ExcludeKnownJets(ts)
	}
	r.Tracks = len(ts)
	nearby := TrackIndex(ts, 15000)
	audible := nearby
	if o.ExcludeKnownJets {
		audible = TrackIndex(allTracks, 15000)
	}
	var corridor []acoustic.Event
	for _, e := range events {
		corridor = append(corridor, acoustic.Event{Start: e.Start, End: e.End, MinSpeed: 0, MaxSpeed: 1000, MaxSlant: 15000})
	}
	seen := map[[2]int64]bool{}
	var all []ConsensusPoint
	grid := func(b geo.Bounds, spacing float64, coarse bool) ([][2]float64, error) {
		points, err := GridLocations(ctx, Config{Bounds: b, RegionMask: o.RegionMask, Grid: spacing, MaxGridPoints: o.MaxPoints, Corridors: coarse, Events: corridor, RecordingStart: o.RecordingStart, ClockUncertainty: o.ClockWindow, MaxGap: o.MaxGap, SoundSpeed: o.SoundSpeed}, ts)
		if err != nil {
			return nil, err
		}
		var kept [][2]float64
		for _, q := range points {
			key := [2]int64{int64(math.Round(q[0] * 1e8)), int64(math.Round(q[1] * 1e8))}
			if !o.Bounds.Contains(q[0], q[1]) || seen[key] {
				continue
			}
			seen[key] = true
			kept = append(kept, q)
		}
		if len(seen) > o.MaxPoints {
			return nil, fmt.Errorf("consensus search exceeds %d points; increase spacing or narrow bounds", o.MaxPoints)
		}
		return kept, nil
	}
	model := CurveModelOptions{Window: o.Window, MaxSourceChange: o.MaxSourceChange}
	scorePoint := func(task consensusTask, height float64) (ConsensusPoint, error) {
		p := ConsensusPoint{Lat: task.lat, Lon: task.lon, Height: height, Spacing: task.spacing}
		local := nearby(geo.ECEF(task.lat, task.lon, height))
		steps := int(math.Round(2 * task.half / task.clockStep))
		for k := 0; k <= steps; k++ {
			clock := task.clock - task.half + float64(k)*task.clockStep
			h := CurveReceiver{ReceiverHypothesis: ReceiverHypothesis{Lat: task.lat, Lon: task.lon, Height: height, RecordingStart: o.RecordingStart, Clock: clock, SoundSpeed: o.SoundSpeed, MaxGap: o.MaxGap}}
			s, err := FitRidgeSubset(ctx, h, local, events, model, o.MinCurves, o.Criteria)
			if err != nil {
				return p, err
			}
			if s.Valid && (!p.Valid || s.Worst < p.Worst) {
				p.Valid, p.Worst, p.Clock, p.Matches, p.Explains, p.Pass = true, s.Worst, clock, s.Matches, s.Explained, s.ReservedPass
			}
		}
		return p, nil
	}
	evaluate := func(stage string, batch []consensusTask) error {
		type result struct {
			p   ConsensusPoint
			err error
		}
		jobs := make(chan consensusTask)
		done := make(chan result, o.Workers)
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()
		var wg sync.WaitGroup
		for w := 0; w < o.Workers; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for task := range jobs {
					height, err := o.Height(task.lat, task.lon)
					if err != nil {
						done <- result{err: err}
						continue
					}
					p, err := scorePoint(task, height)
					done <- result{p: p, err: err}
				}
			}()
		}
		go func() {
			defer close(jobs)
			for _, t := range batch {
				select {
				case jobs <- t:
				case <-ctx.Done():
					return
				}
			}
		}()
		go func() { wg.Wait(); close(done) }()
		n, best := 0, math.Inf(1)
		var first error
		for res := range done {
			if res.err != nil {
				if first == nil {
					first = res.err
					cancel()
				}
				continue
			}
			all = append(all, res.p)
			n++
			if res.p.Valid {
				best = math.Min(best, res.p.Worst)
			}
			if o.Progress != nil && (n == 1 || n%200 == 0 || n == len(batch)) {
				o.Progress(stage, n, len(batch), best)
			}
		}
		if first != nil {
			return first
		}
		return ctx.Err()
	}
	// seeds returns separated valid points, preferring one per distinct
	// aircraft assignment so a single coincidental fit cannot crowd out others.
	seeds := func(separation float64, count int) []ConsensusPoint {
		ordered := append([]ConsensusPoint(nil), all...)
		sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].better(ordered[j]) })
		var out []ConsensusPoint
		groups := map[string]bool{}
		far := func(p ConsensusPoint) bool {
			for _, q := range out {
				if geo.ECEF(p.Lat, p.Lon, 0).Sub(geo.ECEF(q.Lat, q.Lon, 0)).Norm() < separation {
					return false
				}
			}
			return true
		}
		for pass := 0; pass < 2 && len(out) < count; pass++ {
			for _, p := range ordered {
				if !p.Valid || len(out) == count {
					break
				}
				key := p.aircraftKey()
				if pass == 0 && groups[key] || !far(p) {
					continue
				}
				groups[key] = true
				out = append(out, p)
			}
		}
		return out
	}
	coarse, err := grid(o.Bounds, o.Grid, true)
	if err != nil {
		return r, err
	}
	var batch []consensusTask
	for _, q := range coarse {
		batch = append(batch, consensusTask{lat: q[0], lon: q[1], spacing: o.Grid, clock: 0, half: o.ClockWindow, clockStep: o.ClockStep})
	}
	if err := evaluate("grid", batch); err != nil {
		return r, err
	}
	for _, stage := range []struct {
		name                      string
		spacing, radius, separate float64
		count                     int
		half, clockStep           float64
	}{
		// Clock and along-track position trade off, so a coarse seed's best
		// clock can be several coarse steps from the refined optimum.
		// Refinement boxes reach 1.5 cells and seeds sit 1.5 cells apart, so
		// boxes overlap and a true position between seeds is still covered.
		{"refine", o.Refine, 1.5 * o.Grid, 1.5 * o.Grid, o.RefinePeaks, 2 * o.ClockStep, o.FineClockStep},
		{"final", o.Final, 1.5 * o.Refine, 1.5 * o.Refine, o.FinalPeaks, 2 * o.FineClockStep, o.FineClockStep / 2},
	} {
		batch = batch[:0]
		for _, p := range seeds(stage.separate, stage.count) {
			dy := stage.radius / 111320
			dx := dy / math.Cos(p.Lat*math.Pi/180)
			b := geo.Bounds{South: math.Max(o.Bounds.South, p.Lat-dy), North: math.Min(o.Bounds.North, p.Lat+dy), West: math.Max(o.Bounds.West, p.Lon-dx), East: math.Min(o.Bounds.East, p.Lon+dx)}
			if !b.Valid() {
				continue
			}
			points, err := grid(b, stage.spacing, false)
			if err != nil {
				return r, err
			}
			for _, q := range points {
				batch = append(batch, consensusTask{lat: q[0], lon: q[1], spacing: stage.spacing, clock: p.Clock, half: stage.half, clockStep: stage.clockStep})
			}
		}
		if err := evaluate(stage.name, batch); err != nil {
			return r, err
		}
	}
	r.Points, r.Evaluated = all, len(all)
	r.Regions = ConsensusRegions(all, o.RegionSeparation, max(1, o.Regions))
	if o.Loudness != nil {
		for i := range r.Regions {
			g := &r.Regions[i]
			obs := geo.ECEF(g.Lat, g.Lon, g.Height)
			c := CheckLoudness(*o.Loudness, audible(obs), obs, epoch+g.Clock, o.LoudnessOptions)
			g.Loudness = &c
			g.Contradicted = c.Contradicts()
		}
		// Stable: residual order is kept within each group.
		sort.SliceStable(r.Regions, func(i, j int) bool { return !r.Regions[i].Contradicted && r.Regions[j].Contradicted })
	}
	r.Status, r.RunnerUp, r.AgreementRadius = consensusStatus(r.Regions, o.ResolveRatio)
	r.Elapsed = time.Since(started).Seconds()
	return r, nil
}

// ConsensusRegions keeps the best valid point of each area separated by at
// least `separation` metres, best first.
func ConsensusRegions(points []ConsensusPoint, separation float64, limit int) []ConsensusPoint {
	ordered := append([]ConsensusPoint(nil), points...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].better(ordered[j]) })
	var out []ConsensusPoint
	for _, p := range ordered {
		if !p.Valid || len(out) == limit {
			break
		}
		keep := true
		for _, q := range out {
			if geo.ECEF(p.Lat, p.Lon, 0).Sub(geo.ECEF(q.Lat, q.Lon, 0)).Norm() < separation {
				keep = false
				break
			}
		}
		if keep {
			out = append(out, p)
		}
	}
	return out
}

// consensusStatus compares the winner with the best separated region that is
// not contradicted by loudness. Contradicted regions never make a result
// ambiguous, and a contradicted winner is never "resolved".
func consensusStatus(regions []ConsensusPoint, ratio float64) (string, *float64, float64) {
	if len(regions) == 0 {
		return "no_match", nil, 0
	}
	win := regions[0]
	if win.Contradicted {
		return "contradicted", nil, 0
	}
	var margin *float64
	radius := 0.
	for _, g := range regions[1:] {
		if g.Contradicted {
			continue
		}
		m := g.Worst / math.Max(win.Worst, 1e-9)
		if margin == nil {
			margin = &m
		}
		if m < ratio {
			radius = math.Max(radius, geo.ECEF(g.Lat, g.Lon, 0).Sub(geo.ECEF(win.Lat, win.Lon, 0)).Norm())
		}
	}
	if margin == nil || *margin >= ratio {
		return "resolved", margin, radius
	}
	return "ambiguous", margin, radius
}
