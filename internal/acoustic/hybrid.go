package acoustic

import (
	"context"
	"math"
	"sort"
)

const HybridProfile = "hybrid"
const HybridVersion = DetectorVersion + "/hybrid-v2"

// AnalyzeHybrid combines long narrowband curves with short higher-frequency
// sweeps. It classifies acoustic evidence, not propeller/jet identities. The
// scoring spectrum spans both bands at the shorter FFT resolution.
func AnalyzeHybrid(ctx context.Context, path string) (*Spectrum, DetectionReport, error) {
	_, standard, err := AnalyzeMultiResolution(ctx, path)
	if err != nil {
		return nil, DetectionReport{}, err
	}
	o := ShortPassOptions()
	s, short, err := analyzeShortPassBand(ctx, path, o, 40)
	if err != nil {
		return nil, DetectionReport{}, err
	}
	// Keep the original short-pass detector's 100 Hz normalization, while its
	// separate matching spectrum covers the long profile's lower tones too.
	return s, MergeProfiles(standard, short), ctx.Err()
}

// MergeProfiles preserves actual measured paths and their original quality
// checks. It does not reconstruct long curves on a shorter/noisier FFT grid.
func MergeProfiles(reports ...DetectionReport) DetectionReport {
	r := DetectionReport{Version: HybridVersion, Rejected: map[string]int{}}
	var all []Event
	for _, d := range reports {
		r.ScannedEnd = math.Max(r.ScannedEnd, d.ScannedEnd)
		all = append(all, d.Events...)
		for k, n := range d.Rejected {
			r.Rejected[k] += n
		}
	}
	sort.SliceStable(all, func(i, j int) bool {
		if evidenceRank(all[i]) != evidenceRank(all[j]) {
			return evidenceRank(all[i]) > evidenceRank(all[j])
		}
		return all[i].End-all[i].Start > all[j].End-all[j].Start
	})
	var kept []Event
	for _, e := range all {
		duplicate := false
		for _, other := range kept {
			overlap := math.Min(e.End, other.End) - math.Max(e.Start, other.Start)
			if overlap >= .65*math.Min(e.End-e.Start, other.End-other.Start) && sameRidge(e, other) {
				duplicate = true
				break
			}
		}
		if duplicate {
			r.Rejected["duplicate_across_profiles"]++
			continue
		}
		kept = append(kept, e)
	}
	sort.SliceStable(kept, func(i, j int) bool { return kept[i].Start < kept[j].Start })
	r.SetEvents(kept)
	return r
}
