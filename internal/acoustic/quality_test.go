package acoustic

import (
	"bytes"
	"encoding/binary"
	"math"
	"math/rand"
	"testing"
)

// Synthesize received phase from independently solved emission time. This
// exercises audio decoding/STFT/ridge finding, including a high fan-like tone,
// without constructing a ridge using the fitter under test.
func TestStrongAcousticEvidenceFromJetSpeedAudio(t *testing.T) {
	const rate, duration = 4096, 120
	const v, d, f0, c = 180.0, 4000.0, 800.0, 343.0
	var pcm bytes.Buffer
	rng := rand.New(rand.NewSource(2026))
	for i := 0; i < rate*duration; i++ {
		arrival := float64(i)/rate - 55
		lo, hi := arrival-200, arrival
		for k := 0; k < 32; k++ {
			te := (lo + hi) / 2
			if te+math.Hypot(d, v*te)/c > arrival {
				hi = te
			} else {
				lo = te
			}
		}
		te := (lo + hi) / 2
		value := .25*math.Sin(2*math.Pi*f0*te) + .005*rng.NormFloat64() + .02*math.Sin(2*math.Pi*60*arrival)
		if err := binary.Write(&pcm, binary.LittleEndian, float32(value)); err != nil {
			t.Fatal(err)
		}
	}
	options := Defaults()
	short, err := FromPCM(bytes.NewReader(pcm.Bytes()), "synthetic jet-speed fan tone", options)
	if err != nil {
		t.Fatal(err)
	}
	options.Window, options.Hop = 16384, 2048
	long, err := FromPCM(bytes.NewReader(pcm.Bytes()), "synthetic jet-speed fan tone", options)
	if err != nil {
		t.Fatal(err)
	}
	report := CombineDetections(long, CorroborateCurves(long.DiscoverAll(), short), CorroborateCurves(short.DiscoverAll(), long))
	selected := SearchEvents(report.Events, true, false)
	if len(selected) != 1 {
		for _, e := range report.Events {
			if e.Evidence.Level != "weak" {
				t.Logf("%.1f–%.1f %+v %+v", e.Start, e.End, e.Evidence, e.Measurement)
			}
		}
		t.Fatalf("expected one jointly usable strong fan-tone curve, got %d of %d", len(selected), len(report.Events))
	}
	e := selected[0]
	if e.Measurement.FMin > f0 || e.Measurement.FMax < f0 || e.Evidence.Resolutions != 2 || e.Evidence.HeldoutRMSE == nil || *e.Evidence.HeldoutRMSE >= *e.Evidence.HeldoutLinearRMSE*.7 {
		t.Fatalf("lost known tone/held-out evidence: %+v %+v", e.Measurement, e.Evidence)
	}
	if e.Measurement.MinSpeed > v || e.Measurement.MaxSpeed < v {
		t.Fatalf("jet speed excluded: %+v", e.Measurement)
	}
}

func TestStrongEvidenceRejectsLinearPitchAndSparseRidges(t *testing.T) {
	s := &Spectrum{SampleRate: 1024, Window: 2048, Hop: 512, FirstBin: 80}
	var points []ridgePoint
	for i := 0; i < 220; i++ {
		hz := 180 - .5*s.Time(i)
		row := make([]float32, 521)
		for k := range row {
			row[k] = float32(25 * math.Exp(-math.Pow((s.Hz(k)-hz)/.4, 2)))
		}
		s.Rows = append(s.Rows, row)
		points = append(points, ridgePoint{frame: i, hz: hz, value: 25})
	}
	m := fitRidge(s, points)
	e := Event{Measurement: &m}
	for _, p := range points {
		e.Ridge = append(e.Ridge, RidgeSample{Time: s.Time(p.frame), Hz: p.hz})
	}
	if q := assessCurve(s, e, points); q.Level != "weak" || q.Predictive {
		t.Fatalf("linear chirp promoted: %+v", q)
	}
	// Even an apparently excellent model cannot make a ridge with missing
	// measurements strong. Do not interpolate absent measurements into evidence.
	m.ShapePass = true
	var sparse []ridgePoint
	for i, p := range points {
		if i%3 != 0 {
			sparse = append(sparse, p)
		}
	}
	if q := assessCurve(s, e, sparse); q.Level != "weak" || q.Coverage >= .85 {
		t.Fatalf("sparse ridge promoted: %+v", q)
	}
}

func TestAutomaticEvidencePolicyForWalkingAndStationary(t *testing.T) {
	complete := &RidgeMeasurement{Usable: true}
	events := []Event{
		{Evidence: &CurveEvidence{Level: "strong"}, Measurement: complete},
		{Evidence: &CurveEvidence{Level: "strong"}, Measurement: &RidgeMeasurement{}},
		{Evidence: &CurveEvidence{Level: "review"}, Measurement: complete},
		{Evidence: &CurveEvidence{Level: "weak"}, Measurement: complete},
		{Measurement: complete}, // legacy detections require reassessment
	}
	for _, tc := range []struct {
		joint, weak bool
		want        int
	}{{false, false, 2}, {true, false, 1}, {false, true, 5}, {true, true, 1}} {
		if got := len(SearchEvents(events, tc.joint, tc.weak)); got != tc.want {
			t.Fatalf("joint=%t weak=%t: got %d want %d", tc.joint, tc.weak, got, tc.want)
		}
	}
}
