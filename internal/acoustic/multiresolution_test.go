package acoustic

import "testing"

func TestMultipleResolutionsMergeHarmonicsAndPreferCompleteCurves(t *testing.T) {
	s := &Spectrum{SampleRate: 1024, Window: 2048, Hop: 512}
	for i := 0; i < 400; i++ {
		s.Rows = append(s.Rows, make([]float32, 20))
	}
	a := Event{Start: 20, End: 100, CPATime: 60, Measurement: &RidgeMeasurement{Usable: false, RMSE: .1, F0: 130}}
	b := a
	b.Measurement = &RidgeMeasurement{Usable: true, RMSE: .4, F0: 260}
	for i := 20; i <= 100; i++ {
		a.Ridge = append(a.Ridge, RidgeSample{float64(i), 150 - float64(i)*.2})
		b.Ridge = append(b.Ridge, RidgeSample{float64(i), 300 - float64(i)*.4})
	}
	late := a
	late.Start = 120
	late.End = 190
	late.Ridge = nil
	r := CombineDetections(s, DetectionReport{Events: []Event{a, late}, ScannedEnd: 200}, DetectionReport{Events: []Event{b}, ScannedEnd: 200})
	if len(r.Events) != 2 || !r.Events[0].Measurement.Usable || r.Events[1].Start != 120 || r.Rejected["duplicate_across_resolutions"] != 1 {
		t.Fatalf("lost late curve or retained duplicate harmonic: %+v", r)
	}
}
