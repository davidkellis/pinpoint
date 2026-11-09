package webapp

import (
	"context"
	"fmt"
	"path/filepath"
	"pinpoint/internal/acoustic"
)

func detectorProfile(p string) (string, error) {
	switch p {
	case "", "standard":
		return "standard", nil
	case acoustic.ShortPassProfile, acoustic.HybridProfile:
		return p, nil
	default:
		return "", fmt.Errorf("Choose long curves, short sweeps, or combined analysis.")
	}
}

func detectorVersion(p string) string {
	switch p {
	case acoustic.ShortPassProfile:
		return acoustic.ShortPassVersion
	case acoustic.HybridProfile:
		return acoustic.HybridVersion
	default:
		return acoustic.DetectorVersion
	}
}

func (s *Server) analyzeProfile(ctx context.Context, j *job, profile string) error {
	var spectrum *acoustic.Spectrum
	var d acoustic.DetectionReport
	var err error
	if profile == acoustic.HybridProfile {
		j.phase("audio", "Checking long tonal curves and short sweeps across the complete recording")
		spectrum, d, err = acoustic.AnalyzeHybrid(ctx, filepath.Join(j.dir, "audio"))
	} else {
		j.phase("audio", "Checking short sweeps up to 6.5 kHz across the complete recording")
		spectrum, d, err = acoustic.AnalyzeShortPass(ctx, filepath.Join(j.dir, "audio"), acoustic.ShortPassOptions())
	}
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
	return s.storeDetection(ctx, j, spectrum, d, nil)
}
