package acoustic

import (
	"context"
	"fmt"
)

// AnalyzeTrainingInterval decodes real samples with recording-relative times.
// Frequency backgrounds use training frames only; each frame's spectral median
// uses only that frame. Validation samples cannot alter training normalization.
func AnalyzeTrainingInterval(ctx context.Context, path string, o Options, from, to float64, training func(float64) bool) (*Spectrum, error) {
	if !finite(from) || !finite(to) || from < 0 || to <= from || training == nil {
		return nil, fmt.Errorf("invalid validation interval")
	}
	s, err := decodeSpan(ctx, path, o, from, to-from, false)
	if err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	if err = s.whitenTraining(training); err != nil {
		return nil, err
	}
	return s, ctx.Err()
}

func (s *Spectrum) whitenTraining(training func(float64) bool) error {
	var frames []int
	for i := range s.Rows {
		if training(s.Time(i)) {
			frames = append(frames, i)
		}
	}
	if len(frames) < 5 {
		return fmt.Errorf("fewer than five guarded training frames")
	}
	col := make([]float64, len(frames))
	for j := range s.Rows[0] {
		for k, i := range frames {
			col[k] = float64(s.Rows[i][j])
		}
		background := float32(median(col))
		for i := range s.Rows {
			s.Rows[i][j] -= background
		}
	}
	row := make([]float64, len(s.Rows[0]))
	for i := range s.Rows {
		for j, v := range s.Rows[i] {
			row[j] = float64(v)
		}
		background := float32(median(row))
		for j := range s.Rows[i] {
			s.Rows[i][j] -= background
		}
	}
	return nil
}
