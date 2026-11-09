package acoustic

import (
	"math"
	"math/rand"
	"testing"
)

func TestAutomaticDetectorRejectsTonesBroadbandAndShortChirps(t *testing.T) {
	s := &Spectrum{SampleRate: 1024, Window: 2048, Hop: 512, FirstBin: 80}
	rng := rand.New(rand.NewSource(5))
	for i := 0; i < 240; i++ {
		row := make([]float32, 521)
		for k := range row {
			row[k] = float32(14 + rng.Float64()*3)
		}
		row[180] = 30
		// An eight-second chirp should not trigger an aircraft search.
		if i >= 80 && i < 96 {
			row[300-(i-80)*3] = 35
		}
		s.Rows = append(s.Rows, row)
	}
	if events := s.Discover(); len(events) != 0 {
		t.Fatalf("false detections: %+v", events)
	}
}

func TestAutomaticDetectorFindsIndependentDopplerRidge(t *testing.T) {
	s := &Spectrum{SampleRate: 1024, Window: 2048, Hop: 512, FirstBin: 80}
	const v = 80.0
	const c = 343.0
	const d = 2000.0
	const f0 = 130.0
	for i := 0; i < 200; i++ {
		row := make([]float32, 521)
		tr := s.Time(i) - 50
		te := (c*c*tr - math.Sqrt(c*c*v*v*tr*tr+(c*c-v*v)*d*d)) / (c*c - v*v)
		f := f0 * c / (c + v*v*te/math.Hypot(d, v*te))
		for k := range row {
			delta := (s.Hz(k) - f) / .4
			row[k] = float32(24 * math.Exp(-delta*delta))
		}
		s.Rows = append(s.Rows, row)
	}
	events := s.Discover()
	if len(events) != 1 {
		t.Fatalf("expected one pass: %+v", events)
	}
	if events[0].FMin > f0 || events[0].FMax < f0 || events[0].End-events[0].Start < 50 {
		t.Fatalf("lost source frequency or sustained event: %+v", events[0])
	}
}

func TestDetectorRetainsAllEventsIncludingQuietLateAndConcurrentPasses(t *testing.T) {
	s := &Spectrum{SampleRate: 1024, Window: 2048, Hop: 512, FirstBin: 80}
	// Twelve separate passes, followed by simultaneous unrelated passes. The
	// final event is quieter than every early pass but must still be retained.
	for frame := 0; frame < 2200; frame++ {
		row := make([]float32, 1000)
		now := s.Time(frame)
		for event := 0; event < 14; event++ {
			center := 40 + float64(event)*75
			f0 := 130.0
			power := 24.0
			if event == 13 {
				center = 940
				f0 = 203
				power = 9
			}
			if math.Abs(now-center) > 30 {
				continue
			}
			const v = 80.0
			const c = 343.0
			const d = 2000.0
			tr := now - center
			te := (c*c*tr - math.Sqrt(c*c*v*v*tr*tr+(c*c-v*v)*d*d)) / (c*c - v*v)
			hz := f0 * c / (c + v*v*te/math.Hypot(d, v*te))
			for k := range row {
				delta := (s.Hz(k) - hz) / .4
				row[k] += float32(power * math.Exp(-delta*delta))
			}
		}
		s.Rows = append(s.Rows, row)
	}
	report := s.DiscoverAll()
	if len(report.Events) != 14 {
		t.Fatalf("expected all 14 independent sounds, got %d; rejected=%v", len(report.Events), report.Rejected)
	}
	if report.ScannedStart != 0 || report.ScannedEnd != s.Time(len(s.Rows)-1)+1 {
		t.Fatal("missing full recording coverage")
	}
	last := report.Events[len(report.Events)-1]
	if last.Start < 900 || !last.Partial || len(last.Ridge) < 20 {
		t.Fatal("late quiet pass or its measured curve lost")
	}
	if report.Windows[len(report.Windows)-1].Candidates != 0 {
		t.Fatal("invented sound in the quiet end of the recording")
	}
}

func TestCrossingRidgesAreNotDeduplicatedAsHarmonics(t *testing.T) {
	a, b := Event{}, Event{}
	for k := 0; k <= 10; k++ {
		a.Ridge = append(a.Ridge, RidgeSample{Time: float64(k), Hz: 160 - float64(k)*2})
		b.Ridge = append(b.Ridge, RidgeSample{Time: float64(k), Hz: 150})
	}
	a.Start = 0
	a.End = 10
	b.Start = 0
	b.End = 10
	if sameRidge(a, b) {
		t.Fatal("discarded an independent curve at a frequency crossing")
	}
	for k := range b.Ridge {
		b.Ridge[k].Hz = a.Ridge[k].Hz * 2
	}
	if !sameRidge(a, b) {
		t.Fatal("did not identify a harmonic copy across the overlap")
	}
	// The tolerance is relative to the harmonic, so modest FFT peak bias on
	// the second harmonic does not invent a second aircraft.
	for k := range b.Ridge {
		b.Ridge[k].Hz *= 1.01
	}
	if !sameRidge(a, b) {
		t.Fatal("counted a biased second harmonic as another flight")
	}
}

func TestLongRidgeIsSplitWithoutLosingItsTail(t *testing.T) {
	s := &Spectrum{SampleRate: 1024, Window: 2048, Hop: 512, FirstBin: 80}
	for frame := 0; frame < 720; frame++ {
		row := make([]float32, 521)
		hz := 180 - s.Time(frame)*.1
		for k := range row {
			delta := (s.Hz(k) - hz) / .4
			row[k] = float32(24 * math.Exp(-delta*delta))
		}
		s.Rows = append(s.Rows, row)
	}
	report := s.DiscoverAll()
	if len(report.Events) < 3 || report.Events[len(report.Events)-1].End < s.Time(719)-2 {
		t.Fatalf("long sound discarded or truncated: %d events", len(report.Events))
	}
}
