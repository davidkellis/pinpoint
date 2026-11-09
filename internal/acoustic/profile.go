package acoustic

import (
	"context"
	"fmt"
)

// ShortPassProfile is experimental. It detects short tonal sweeps, not jet
// identities. Keep it opt-in until labeled field data establish false-positive
// rates; a short car pass or changing engine RPM can resemble aircraft Doppler.
const ShortPassProfile = "short-pass"
const ShortPassVersion = DetectorVersion + "/short-pass-v2"

type discoverySettings struct {
	fragment, shape, gap, cpaFloor, ridgeStep float64
	short                                     bool
}

func (s *Spectrum) settings() discoverySettings {
	if s.discoveryProfile == ShortPassProfile {
		return discoverySettings{4, 6, .5, .25, .125, true}
	}
	return discoverySettings{12, 30, 3, 5, 1, false}
}

// DiscoverProfile leaves the spectrum and the default detector unchanged.
// Short windows are necessary to resolve fast sweeps and leave independent
// training/validation blocks after the FFT overlap guards are removed.
func (s *Spectrum) DiscoverProfile(profile string) (DetectionReport, error) {
	if profile != "" && profile != "standard" && profile != ShortPassProfile {
		return DetectionReport{}, fmt.Errorf("unknown detector profile %q", profile)
	}
	if profile == ShortPassProfile && (s.SampleRate <= 0 || float64(s.Window)/float64(s.SampleRate) > 1) {
		return DetectionReport{}, fmt.Errorf("short-pass requires an FFT window of at most one second")
	}
	copy := *s
	copy.discoveryProfile = profile
	return copy.DiscoverAll(), nil
}

// ShortPassOptions covers fan tones above the normal 1.8 kHz band. The FFT
// lasts half a second, with a one-second pass for corroboration.
func ShortPassOptions() Options {
	return Options{Rate: 16384, Window: 8192, Hop: 1024, MinHz: 100, MaxHz: 6500}
}

// AnalyzeShortPass uses both resolutions of the same audio. Corroboration is
// not an independent aircraft observation. All original quality checks remain.
func AnalyzeShortPass(ctx context.Context, path string, o Options) (*Spectrum, DetectionReport, error) {
	return analyzeShortPassBand(ctx, path, o, o.MinHz)
}

func analyzeShortPassBand(ctx context.Context, path string, o Options, matchingMinHz float64) (*Spectrum, DetectionReport, error) {
	if float64(o.Window)/float64(o.Rate) > 1 {
		return nil, DetectionReport{}, fmt.Errorf("short-pass requires an FFT window of at most one second")
	}
	if duration, ok := needsBlocks(ctx, path, o, 60_000_000); ok {
		return analyzeBlocks(ctx, path, o, duration, true, matchingMinHz)
	}
	s, d, err := analyzeShortSpan(ctx, path, o, 0, 0)
	if err != nil {
		return nil, DetectionReport{}, err
	}
	if matchingMinHz != o.MinHz {
		o.MinHz = matchingMinHz
		reportAnalysis(ctx, AnalysisProgress{"combined", "full-band scoring spectrum", 1, 1, 0, d.ScannedEnd})
		s, err = Analyze(ctx, path, o)
	}
	return s, d, err
}

func analyzeShortSpan(ctx context.Context, path string, o Options, from, duration float64) (*Spectrum, DetectionReport, error) {
	s, err := analyzeSpan(ctx, path, o, from, duration)
	if err != nil {
		return nil, DetectionReport{}, err
	}
	other := o
	other.Window *= 2
	other.Hop *= 2
	if other.Window > o.Rate {
		other.Window, other.Hop = o.Window/2, max(1, o.Hop/2)
	}
	u, err := analyzeSpan(ctx, path, other, from, duration)
	if err != nil {
		return nil, DetectionReport{}, err
	}
	a, err := s.DiscoverProfile(ShortPassProfile)
	if err != nil {
		return nil, DetectionReport{}, err
	}
	b, err := u.DiscoverProfile(ShortPassProfile)
	if err != nil {
		return nil, DetectionReport{}, err
	}
	primary := *s
	primary.discoveryProfile = ShortPassProfile
	a = extendCandidates(&primary, a)
	secondary := *u
	secondary.discoveryProfile = ShortPassProfile
	b = extendCandidates(&secondary, b)
	d := CombineDetections(&primary, CorroborateCurves(a, u), CorroborateCurves(b, s))
	d.Version = ShortPassVersion
	return s, d, ctx.Err()
}
