package acoustic

import (
	"context"
	"math"
	"sort"
)

// AnalyzeMultiResolution uses the same 2 + 4 second analysis as the web app.
// It returns the four-second spectrum used for trajectory scoring. Detection
// thresholds are unchanged; corroboration uses both measured resolutions.
func AnalyzeMultiResolution(ctx context.Context, path string) (*Spectrum, DetectionReport, error) {
	long, _, report, err := AnalyzeWithSpectra(ctx, path)
	return long, report, err
}

// AnalyzeWithSpectra also retains the independent FFT resolution for reviewing
// and refining measured ridges. No telemetry is used during acoustic analysis.
func AnalyzeWithSpectra(ctx context.Context, path string) (*Spectrum, *Spectrum, DetectionReport, error) {
	o := Defaults()
	longOptions := o
	longOptions.Window, longOptions.Hop = 16384, 2048
	if duration, ok := needsBlocks(ctx, path, longOptions, 70_000_000); ok {
		s, d, err := analyzeBlocks(ctx, path, longOptions, duration, false, longOptions.MinHz)
		return s, nil, d, err
	}
	return analyzeStandardSpan(ctx, path, 0, 0)
}

func analyzeStandardSpan(ctx context.Context, path string, from, duration float64) (*Spectrum, *Spectrum, DetectionReport, error) {
	o := Defaults()
	short, err := analyzeSpan(ctx, path, o, from, duration)
	if err != nil {
		return nil, nil, DetectionReport{}, err
	}
	o.Window, o.Hop = 16384, 2048
	long, err := analyzeSpan(ctx, path, o, from, duration)
	if err != nil {
		return nil, nil, DetectionReport{}, err
	}
	longReport, shortReport := long.DiscoverAll(), short.DiscoverAll()
	if err = ctx.Err(); err != nil {
		return nil, nil, DetectionReport{}, err
	}
	return long, short, CombineDetections(long, CorroborateCurves(longReport, short), CorroborateCurves(shortReport, long)), nil
}

// CombineDetections tests short-window ridges as well as narrow frequency bins
// from the long window. A fast chirp can be smeared out by a long FFT. Harmonic
// copies are merged across resolutions; complete informative fits take priority.
func CombineDetections(primary *Spectrum, reports ...DetectionReport) DetectionReport {
	r := DetectionReport{Version: DetectorVersion, Rejected: map[string]int{}}
	var all []Event
	for _, d := range reports {
		r.ScannedEnd = math.Max(r.ScannedEnd, d.ScannedEnd)
		for key, n := range d.Rejected {
			r.Rejected[key] += n
		}
		all = append(all, d.Events...)
	}
	for i := range all {
		if all[i].Evidence == nil {
			continue
		}
		q := *all[i].Evidence
		q.Reasons = append([]string(nil), q.Reasons...)
		for j := range all {
			if all[i].Resolution == all[j].Resolution {
				continue
			}
			overlap := math.Min(all[i].End, all[j].End) - math.Max(all[i].Start, all[j].Start)
			if overlap >= .65*(all[i].End-all[i].Start) && sameRidge(all[i], all[j]) {
				q.Resolutions = 2
				break
			}
		}
		finishEvidence(&q)
		all[i].Evidence = &q
	}
	all = joinCurves(primary, all)
	sort.SliceStable(all, func(i, j int) bool {
		if evidenceRank(all[i]) != evidenceRank(all[j]) {
			return evidenceRank(all[i]) > evidenceRank(all[j])
		}
		a, b := all[i].Measurement, all[j].Measurement
		if a == nil || b == nil {
			return a != nil
		}
		if a.Usable != b.Usable {
			return a.Usable
		}
		// Prefer the supported extent of a full pass over a tiny, nearly
		// perfect fragment of the same tone.
		support := func(e Event) float64 {
			return (e.End - e.Start) * e.Evidence.Coverage / (1 + e.Measurement.RMSE/math.Max(1.5, e.Measurement.F0*.015))
		}
		if all[i].Evidence != nil && all[j].Evidence != nil && evidenceRank(all[i]) > 0 {
			return support(all[i]) > support(all[j])
		}
		return a.RMSE/a.F0 < b.RMSE/b.F0
	})
	var selected []Event
	for _, e := range all {
		e.Start = math.Max(e.Start, primary.Time(0))
		e.End = math.Min(e.End, primary.Time(len(primary.Rows)-1))
		if e.End-e.Start < 5 {
			continue
		}
		e.CPATime = math.Max(e.Start, math.Min(e.End, e.CPATime))
		var ridge []RidgeSample
		for _, p := range e.Ridge {
			if p.Time >= e.Start && p.Time <= e.End {
				ridge = append(ridge, p)
			}
		}
		e.Ridge = ridge
		duplicate := false
		for _, q := range selected {
			if math.Min(e.End, q.End)-math.Max(e.Start, q.Start) >= .65*math.Min(e.End-e.Start, q.End-q.Start) && sameRidge(e, q) {
				duplicate = true
				break
			}
		}
		if duplicate {
			r.Rejected["duplicate_across_resolutions"]++
			continue
		}
		selected = append(selected, e)
	}
	sort.SliceStable(selected, func(i, j int) bool { return selected[i].Start < selected[j].Start })
	r.SetEvents(selected)
	return r
}
