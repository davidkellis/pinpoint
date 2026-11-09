package webapp

import (
	"context"
	"path/filepath"
	"pinpoint/internal/acoustic"
)

func (s *Server) detectRecording(ctx context.Context, j *job, primary *acoustic.Spectrum) (acoustic.DetectionReport, error) {
	if primary.Hop >= primary.SampleRate {
		_, d, err := acoustic.AnalyzeMultiResolution(ctx, filepath.Join(j.dir, "audio"))
		return d, err
	}
	shortPath := filepath.Join(j.dir, "detection-spectrum.bin")
	options := acoustic.Defaults()
	// Existing jobs keep their original analysis band. Re-upload to analyze
	// a wider band; never score new high-frequency ridges against an old cache.
	options.MinHz = primary.Hz(0)
	options.MaxHz = primary.Hz(len(primary.Rows[0]) - 1)
	short, err := acoustic.Load(shortPath)
	if err != nil || short.Hz(0) != options.MinHz || short.Hz(len(short.Rows[0])-1) != options.MaxHz {
		short, err = acoustic.Analyze(ctx, filepath.Join(j.dir, "audio"), options)
		if err != nil {
			return acoustic.DetectionReport{}, err
		}
		if err = short.Save(shortPath); err != nil {
			return acoustic.DetectionReport{}, err
		}
	}
	longReport, shortReport := primary.DiscoverAll(), short.DiscoverAll()
	if err = ctx.Err(); err != nil {
		return acoustic.DetectionReport{}, err
	}
	return acoustic.CombineDetections(primary, acoustic.CorroborateCurves(longReport, short), acoustic.CorroborateCurves(shortReport, primary)), nil
}
