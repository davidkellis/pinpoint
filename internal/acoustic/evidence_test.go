package acoustic

import (
	"bytes"
	"encoding/base64"
	"image/png"
	"strings"
	"testing"
)

func TestEvidenceImageKeepsTimeAndFrequencyAxes(t *testing.T) {
	s := &Spectrum{SampleRate: 1024, Window: 1024, Hop: 1024, FirstBin: 80}
	for i := 0; i < 30; i++ {
		row := make([]float32, 60)
		row[20] = 30
		s.Rows = append(s.Rows, row)
	}
	e := Event{Start: 5.5, End: 20.5, FMin: 95, FMax: 105, MaxSpeed: 100, Ridge: []RidgeSample{{Time: 5.5, Hz: 100}, {Time: 20.5, Hz: 100}}}
	view, err := s.EventImage(e)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(view.DataURL, "data:image/png;base64,"))
	if err != nil {
		t.Fatal(err)
	}
	image, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	y := int((view.MaxHz - 100) / (view.MaxHz - view.MinHz) * float64(image.Bounds().Dy()))
	r, _, _, _ := image.At(2, y).RGBA()
	other, _, _, _ := image.At(2, 0).RGBA()
	if r <= other || view.Start > s.Time(0) || view.End < e.End {
		t.Fatal("spectral axes reversed or event cropped incorrectly")
	}
	e.End = 1000
	if _, err = s.EventImage(e); err == nil {
		t.Fatal("accepted an invalid event window")
	}
}
