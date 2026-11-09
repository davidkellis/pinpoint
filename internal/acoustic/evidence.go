package acoustic

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"math"
)

// SpectralImage contains a focused, axis-calibrated view of measured spectral
// power. It is independent of candidate flight curves and is never synthesized
// from those predictions.
type SpectralImage struct {
	Start   float64 `json:"start_s"`
	End     float64 `json:"end_s"`
	MinHz   float64 `json:"min_hz"`
	MaxHz   float64 `json:"max_hz"`
	DataURL string  `json:"data_url"`
}

func (s *Spectrum) EventImage(e Event) (SpectralImage, error) {
	if len(s.Rows) < 2 || len(s.Rows[0]) < 2 || !finite(e.Start) || !finite(e.End) || e.Start < s.Time(0) || e.End > s.Time(len(s.Rows)-1) || e.End <= e.Start {
		return SpectralImage{}, fmt.Errorf("event is outside the spectrogram")
	}

	low, high := math.Inf(1), math.Inf(-1)
	for _, p := range e.Ridge {
		low = math.Min(low, p.Hz)
		high = math.Max(high, p.Hz)
	}
	if len(e.Ridge) == 0 {
		low = e.FMin / (1 + e.MaxSpeed/343)
		high = e.FMax / (1 - e.MaxSpeed/343)
	}
	margin := math.Max(8, (high-low)*.12)
	low -= margin
	high += margin
	first := max(0, int(math.Floor((e.Start-5-s.Time(0))/s.Step())))
	last := min(len(s.Rows)-1, int(math.Ceil((e.End+5-s.Time(0))/s.Step())))
	df := float64(s.SampleRate) / float64(s.Window)
	bottom := max(0, min(len(s.Rows[0])-2, int(math.Floor(low/df))-s.FirstBin))
	top := max(bottom+1, min(len(s.Rows[0])-1, int(math.Ceil(high/df))-s.FirstBin))
	width, height := min(1600, last-first+1), min(600, top-bottom+1)
	im := image.NewGray(image.Rect(0, 0, width, height))
	for x := 0; x < width; x++ {
		a, b := first+x*(last-first+1)/width, first+(x+1)*(last-first+1)/width
		for y := 0; y < height; y++ {
			lo, hi := bottom+y*(top-bottom+1)/height, bottom+(y+1)*(top-bottom+1)/height
			power := -10.0
			for frame := a; frame < b; frame++ {
				for k := lo; k < hi; k++ {
					power = math.Max(power, float64(s.Rows[frame][k]))
				}
			}
			level := math.Max(0, math.Min(1, (power+5)/35))
			im.SetGray(x, height-y-1, color.Gray{Y: uint8(255 * level)})
		}
	}
	var buf bytes.Buffer
	if err := encodePreview(&buf, im); err != nil {
		return SpectralImage{}, err
	}
	return SpectralImage{s.Time(first) - s.Step()/2, s.Time(last) + s.Step()/2, s.Hz(bottom) - df/2, s.Hz(top) + df/2, "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())}, nil
}
