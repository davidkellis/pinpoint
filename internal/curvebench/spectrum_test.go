package curvebench

import (
	"bytes"
	"context"
	"encoding/binary"
	"math"
	"testing"

	"pinpoint/internal/acoustic"
)

func TestBaselineMatchesProductionSTFT(t *testing.T) {
	audio := make([]float64, Rate*12)
	var pcm bytes.Buffer
	for i := range audio {
		v := float32(.1*math.Sin(2*math.Pi*123.37*float64(i)/Rate) + .02*math.Sin(2*math.Pi*300*float64(i)/Rate))
		audio[i] = float64(v)
		if err := binary.Write(&pcm, binary.LittleEndian, v); err != nil {
			t.Fatal(err)
		}
	}
	o := acoustic.Defaults()
	want, err := acoustic.FromPCM(&pcm, "fixture", o)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := transform(context.Background(), audio, 2, false)
	if err != nil {
		t.Fatal(err)
	}
	got := whiten(raw)
	if len(got.Rows) != len(want.Rows) || got.Time(0) != want.Time(0) {
		t.Fatal("time grid changed")
	}
	for i, row := range got.Rows {
		for k, v := range row {
			if math.Abs(float64(v-want.Rows[i][k])) > 1e-4 {
				t.Fatalf("different STFT/whitening at %d,%d: %g versus %g", i, k, v, want.Rows[i][k])
			}
		}
	}
}
func TestFrequencyReassignmentConcentratesOffBinTone(t *testing.T) {
	const f = 123.37
	audio := make([]float64, Rate*8)
	for i := range audio {
		audio[i] = math.Sin(2 * math.Pi * f * float64(i) / Rate)
	}
	ordinary, err := transform(context.Background(), audio, 1, false)
	if err != nil {
		t.Fatal(err)
	}
	focused, err := transform(context.Background(), audio, 1, true)
	if err != nil {
		t.Fatal(err)
	}
	moments := func(s *acoustic.Spectrum) (float64, float64, float64) {
		sum, weighted, variance := 0., 0., 0.
		for k, v := range s.Rows[10] {
			p := math.Pow(10, float64(v)/10)
			sum += p
			weighted += p * s.Hz(k)
			variance += p * math.Pow(s.Hz(k)-f, 2)
		}
		return weighted / sum, variance / sum, sum
	}
	_, before, energyA := moments(ordinary)
	center, after, energyB := moments(focused)
	if math.Abs(center-f) > .02 || after >= before*.8 || math.Abs(energyA-energyB)/energyA > 1e-4 {
		t.Fatalf("invalid concentration: center %.4f variance %.4f→%.4f energy %g→%g", center, before, after, energyA, energyB)
	}
}
func TestMatchingCannotConfuseDifferentCurveWithTruth(t *testing.T) {
	truth := []acoustic.RidgeSample{{Time: 20, Hz: 180}, {Time: 40, Hz: 160}, {Time: 60, Hz: 120}, {Time: 80, Hz: 100}, {Time: 100, Hz: 90}}
	bad := acoustic.Event{Ridge: []acoustic.RidgeSample{{Time: 20, Hz: 180}, {Time: 40, Hz: 180}, {Time: 60, Hz: 180}, {Time: 80, Hz: 180}, {Time: 100, Hz: 180}}, Evidence: &acoustic.CurveEvidence{Level: "strong"}}
	if got := match([]acoustic.Event{bad}, truth); got.Recovered {
		t.Fatal("unrelated strong tone counted as known flyover")
	}
	good := acoustic.Event{Ridge: make([]acoustic.RidgeSample, len(truth)), Evidence: &acoustic.CurveEvidence{Level: "strong"}}
	for i, p := range truth {
		good.Ridge[i] = acoustic.RidgeSample{Time: p.Time, Hz: 2 * p.Hz}
	}
	if got := match([]acoustic.Event{bad, good}, truth); !got.Recovered || !got.Strong || got.Ratio != 2 {
		t.Fatalf("missed measured harmonic: %+v", got)
	}
}
func TestInjectionSNRAndArrivalPhase(t *testing.T) {
	audio := make([]float64, Rate*40)
	for i := range audio {
		audio[i] = .1 * math.Sin(2*math.Pi*60*float64(i)/Rate)
	}
	mixed, truth := inject(audio, "jet", -25)
	noise, added := 0., 0.
	for i := range audio {
		noise += audio[i] * audio[i]
		d := mixed[i] - audio[i]
		added += d * d
	}
	if math.Abs(10*math.Log10(added/noise)+25) > 1e-8 {
		t.Fatal("incorrect injection RMS SNR")
	}
	if len(truth) != 1 || math.Abs(truth[0].Hz-800) > 1e-6 {
		t.Fatalf("arrival CPA does not recover source frequency: %+v", truth)
	}
}

func TestRecoveredStrongPathIsNotHiddenByWeakPerfectPath(t *testing.T) {
	truth := []acoustic.RidgeSample{{Time: 20, Hz: 180}, {Time: 40, Hz: 160}, {Time: 60, Hz: 120}, {Time: 80, Hz: 100}, {Time: 100, Hz: 90}}
	strong := acoustic.Event{Ridge: append([]acoustic.RidgeSample(nil), truth...), Evidence: &acoustic.CurveEvidence{Level: "strong"}}
	weak := acoustic.Event{Ridge: append([]acoustic.RidgeSample(nil), truth...), Evidence: &acoustic.CurveEvidence{Level: "weak"}}
	strong.Ridge[4].Hz = 80
	for _, events := range [][]acoustic.Event{{weak, strong}, {strong, weak}} {
		got := match(events, truth)
		if !got.Strong || got.HitFraction != .8 {
			t.Fatalf("lost qualifying strong recovery: %+v", got)
		}
	}
}
