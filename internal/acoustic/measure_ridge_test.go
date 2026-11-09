package acoustic

import (
	"math"
	"testing"
)

func measuredHarmonicFixture() (*Spectrum, Event, func(float64) float64) {
	s := &Spectrum{SampleRate: 4096, Window: 8192, Hop: 2048, FirstBin: 80}
	e := Event{Start: 1, End: 30.5, Evidence: &CurveEvidence{Level: "strong"}, Measurement: &RidgeMeasurement{ShapePass: true}}
	frequency := func(sec float64) float64 { return 107.17 - .18*sec }
	for i := 0; i < 60; i++ {
		sec := s.Time(i)
		row := make([]float32, 1520)
		for k := range row {
			row[k] = -10
		}
		for h := 1; h <= 4; h++ {
			center := frequency(sec)*float64(h)/.5 - 80
			for k := max(0, int(center)-2); k <= min(len(row)-1, int(center)+2); k++ {
				row[k] = float32(24 - 7*math.Pow(float64(k)-center, 2))
			}
		}
		s.Rows = append(s.Rows, row)
		e.Ridge = append(e.Ridge, RidgeSample{sec, math.Round(frequency(sec)*2) / 2})
	}
	return s, e, frequency
}

func TestRidgeSamplingUsesMeasuredHarmonicsAndClearsInheritedClaims(t *testing.T) {
	s, e, f := measuredHarmonicFixture()
	out, r, err := MeasureRidgeSamples(s, e, 4)
	if err != nil || r.Measured != len(e.Ridge) || r.Multiple != len(e.Ridge) {
		t.Fatal("lost measured peaks", r, err)
	}
	if out.Evidence != nil || out.Measurement != nil || out.Start != e.Start || out.End != e.End {
		t.Fatal("inherited confidence or changed audio window")
	}
	for _, p := range out.Ridge {
		if math.Abs(p.Hz-f(p.Time)) > 1e-5 {
			t.Fatal("sampling replaced measurements with bins or an ideal curve", p)
		}
	}
	if e.Evidence == nil || e.Measurement == nil {
		t.Fatal("mutated input evidence")
	}
}

func TestRidgeSamplingDoesNotInventAbsentOrCroppedEvidence(t *testing.T) {
	for _, mode := range []string{"absent", "middle-only"} {
		t.Run(mode, func(t *testing.T) {
			s, e, _ := measuredHarmonicFixture()
			for i, row := range s.Rows {
				if mode == "absent" || i < 10 || i > 48 {
					for k := range row {
						row[k] = -10
					}
				}
			}
			if _, _, err := MeasureRidgeSamples(s, e, 4); err == nil {
				t.Fatal("accepted missing sound or discarded endpoints")
			}
		})
	}
	if _, ok := RefineMeasuredCurve(nil, nil, Event{}); ok {
		t.Fatal("refined absent sound")
	}
}
