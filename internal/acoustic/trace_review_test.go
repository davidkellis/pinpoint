package acoustic

import (
	"context"
	"math"
	"testing"
)

func TestReviewedTracesFollowBothRealFlyovers(t *testing.T) {
	merge := func(resolution string) *Spectrum {
		a, b := loadRealSpectrum(t, "colsto-1-"+resolution), loadRealSpectrum(t, "colsto-2-"+resolution)
		base := int(940 / a.Step())
		rows := make([][]float32, base+len(b.Rows))
		for i := range rows {
			rows[i] = make([]float32, len(a.Rows[0]))
		}
		copy(rows, a.Rows)
		copy(rows[base:], b.Rows)
		out := *a
		out.Rows = rows
		return &out
	}
	a, b := merge("long"), merge("short")
	d := CombineDetections(a, CorroborateCurves(a.DiscoverAll(), b), CorroborateCurves(b.DiscoverAll(), a))
	windows := []Event{{Start: 100, End: 280, FMin: 105, FMax: 125, FStep: .5, MinSpeed: 110, MaxSpeed: 170, MinSlant: 3500, MaxSlant: 9000}, {Start: 1040, End: 1140, FMin: 120, FMax: 170, FStep: .5, MinSpeed: 60, MaxSpeed: 180, MaxSlant: 12000}}
	r, err := TraceWindows(context.Background(), a, b, d, windows)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Events) != 2 || math.Abs((r.Events[1].CPATime-r.Events[0].CPATime)-921) > 10 {
		t.Fatal("lost the observed flyover offset")
	}
	anchors := [][]RidgeSample{{{128, 144}, {140, 131.5}, {154, 114}, {168, 102}, {200, 89}}, {{1050, 168}, {1060, 155.5}, {1070, 138}, {1080, 123.5}, {1090, 116}, {1100, 110}}}
	for i, e := range r.Events {
		if e.End-e.Start < 80 || e.Evidence.Level != "strong" || e.FMin != windows[i].FMin || e.MaxSlant != windows[i].MaxSlant {
			t.Fatal("weakened evidence or changed reviewed physical bounds")
		}
		for _, p := range anchors[i] {
			hz, ok := e.RidgeHz(p.Time)
			if !ok || math.Abs(hz-p.Hz) > 3 {
				t.Fatalf("lost measured anchor %+v: %g", p, hz)
			}
		}
		t.Logf("window %d: %.1f–%.1f, polished %v, residual %.3fHz", i+1, e.Start, e.End, r.Decisions[i].Polished, e.Measurement.RMSE)
	}
	// An ambiguous acoustic choice must not be resolved by downstream flight scores.
	for _, e := range d.Events {
		if e.Start < 200 && e.End > 200 && e.Evidence.Level == "strong" && e.Measurement.F0 > 105 && e.Measurement.F0 < 125 {
			d.Events = append(d.Events, e)
			break
		}
	}
	if _, err = TraceWindows(context.Background(), a, b, d, windows); err == nil {
		t.Fatal("silently selected one of two eligible curves")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = TraceWindows(ctx, a, b, d, windows); err != context.Canceled {
		t.Fatal("ignored cancellation")
	}
}

func TestSubBinPeakUsesMeasuredParabolicMaximum(t *testing.T) {
	s := &Spectrum{SampleRate: 1024, Window: 1024, Rows: [][]float32{make([]float32, 30)}}
	for k := range s.Rows[0] {
		s.Rows[0][k] = float32(20 - 5*math.Pow(float64(k)-10.3, 2))
	}
	if got := refinedPeakHz(s, 0, 10); math.Abs(got-10.3) > 1e-5 {
		t.Fatalf("sub-bin peak = %g", got)
	}
}

func TestReviewedTracingDoesNotPromoteWeakSounds(t *testing.T) {
	s := &Spectrum{SampleRate: 1024, Window: 1024, Hop: 512, Rows: [][]float32{make([]float32, 300)}}
	windows := []Event{{Start: 1, End: 31, FMin: 90, FMax: 120}, {Start: 100, End: 130, FMin: 90, FMax: 120}}
	d := DetectionReport{Events: []Event{{Start: 1, End: 31, Evidence: &CurveEvidence{Level: "weak"}, Measurement: &RidgeMeasurement{ShapePass: true, F0: 110}}}}
	if _, err := TraceWindows(context.Background(), s, nil, d, windows); err == nil {
		t.Fatal("promoted weak candidate into joint localization")
	}
}
