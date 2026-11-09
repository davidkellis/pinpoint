package search

import (
	"context"
	"fmt"
	"math"
	"testing"
	"time"

	"pinpoint/internal/acoustic"
	"pinpoint/internal/geo"
	"pinpoint/internal/tracks"
)

// The injected frequency uses an analytic retarded-time expression independent
// of the trajectory solver. This is a scoring regression, not an audio corpus.
func recoveryFixture(length int, arrivalCPA, tone, hum float64) (*acoustic.Spectrum, []tracks.Track, ReceiverHypothesis, GuidedOptions) {
	return recoveryFixturePitch(length, arrivalCPA, tone, hum, 160)
}

func recoveryFixturePitch(length int, arrivalCPA, tone, hum, source float64) (*acoustic.Spectrum, []tracks.Track, ReceiverHypothesis, GuidedOptions) {
	const epoch = 1700000000.
	const c = 343.
	const distance = 1500.
	const speed = 85.
	emissionCPA := arrivalCPA - distance/c
	s := &acoustic.Spectrum{Version: 1, SampleRate: 16384, Window: 16384, Hop: 16384, FirstBin: 40, Rows: make([][]float32, length)}
	for i := range s.Rows {
		row := make([]float32, max(300, int(source*2.2)))
		s.Rows[i] = row
		u := s.Time(i) - emissionCPA
		te := (c*c*u - math.Sqrt(c*c*speed*speed*u*u+(c*c-speed*speed)*distance*distance)) / (c*c - speed*speed)
		f := source * c / (c + speed*speed*te/math.Sqrt(distance*distance+speed*speed*te*te))
		for k := range row {
			if math.Abs(s.Time(i)-arrivalCPA) <= 20.5 {
				row[k] = float32(tone * math.Exp(-math.Pow((s.Hz(k)-f)/math.Max(.8, source*.005), 2)))
			}
			row[k] += float32(hum * math.Exp(-math.Pow((s.Hz(k)-80)/2, 2)))
		}
	}
	obs := geo.ECEF(0, 0, 0)
	track := tracks.Track{ID: "synthetic-aircraft"}
	for sec := -300.; sec <= 1000; sec += 2 {
		p := obs.Add(geo.Vec{X: distance, Y: speed * (sec - emissionCPA)})
		lat, lon, height := geo.Geodetic(p)
		track.Points = append(track.Points, tracks.Point{Time: epoch + sec, Lat: lat, Lon: lon, Alt: height, Position: p})
	}
	h := ReceiverHypothesis{RecordingStart: time.Unix(int64(epoch), 0).UTC().Format(time.RFC3339), SoundSpeed: c, MaxGap: 30}
	o := DefaultGuidedOptions()
	o.MinHz = 60
	o.MaxHz = 220
	o.StepHz = .5
	return s, []tracks.Track{track}, h, o
}

func TestDefaultGuidedFindsHighFrequencyCurvedTone(t *testing.T) {
	s, ts, h, _ := recoveryFixturePitch(80, 40, 25, 0, 1600)
	r, err := ReviewReceiver(context.Background(), h, s, ts, DefaultGuidedOptions())
	if err != nil || r.Supported != 1 || len(r.Matches) != 1 || math.Abs(r.Matches[0].Fit.F0-1600) > 3 {
		t.Fatalf("default follow-up lost a high-frequency flyover tone: %+v %v", r.Matches, err)
	}
}

func TestGuidedSelectionRecoversCurveBehindSteadyHum(t *testing.T) {
	s, ts, h, o := recoveryFixture(800, 184.3731778, 6, 20)
	r, err := ReviewReceiver(context.Background(), h, s, ts, o)
	if err != nil {
		t.Fatal(err)
	}
	if r.Supported != 1 || len(r.Matches) != 1 || math.Abs(r.Matches[0].Fit.F0-160) > 1 {
		t.Fatalf("failed to recover known curve in clutter: %+v", r.Matches)
	}
}

func TestGuidedShortAndBoundaryPassesUseSameAudioControls(t *testing.T) {
	for _, center := range []float64{40, 12.5} {
		s, ts, h, o := recoveryFixture(80, center, 25, 0)
		r, err := ReviewReceiver(context.Background(), h, s, ts, o)
		if err != nil {
			t.Fatal(err)
		}
		if r.Supported != 1 || len(r.Matches) != 1 {
			t.Fatalf("center %g: %+v", center, r.Matches)
		}
		m := r.Matches[0]
		if m.ControlMode != "same_audio_geometry" || len(m.Alternatives) < 3 || m.Linear == nil || !m.Linear.Valid || m.Validation != "experimental_support" {
			t.Fatalf("missing controls: %+v", m)
		}
		if center == 12.5 && !m.Partial {
			t.Fatal("boundary window was not reported as partial")
		}
	}
}

func TestGuidedShortControlsDoNotPromoteAbsentOrSteadySound(t *testing.T) {
	for _, hum := range []float64{0, 20} {
		s, ts, h, o := recoveryFixture(80, 40, 0, hum)
		r, err := ReviewReceiver(context.Background(), h, s, ts, o)
		if err != nil || r.Supported != 0 {
			t.Fatalf("invented support for hum %g: %+v %v", hum, r.Matches, err)
		}
	}
}

func TestGuidedUnavailableValidationIsExplicit(t *testing.T) {
	s, ts, h, o := recoveryFixture(12, 6, 25, 0)
	r, err := ReviewReceiver(context.Background(), h, s, ts, o)
	if err != nil {
		t.Fatal(err)
	}
	if r.Supported != 0 || len(r.Matches) != 1 || r.Matches[0].Validation != "unavailable" {
		t.Fatalf("missing unavailable status: %+v", r)
	}
}

func TestGuidedRejectsStraightChirpInShortAndLongRecordings(t *testing.T) {
	for _, length := range []int{80, 800} {
		t.Run(fmt.Sprint(length), func(t *testing.T) {
			center := float64(length) / 2
			s, ts, h, o := recoveryFixture(length, center, 0, 0)
			for i, row := range s.Rows {
				if math.Abs(s.Time(i)-center) > 20.5 {
					continue
				}
				f := 160 - 2*(s.Time(i)-center)
				for k := range row {
					row[k] = float32(20 * math.Exp(-math.Pow((s.Hz(k)-f)/.8, 2)))
				}
			}
			r, err := ReviewReceiver(context.Background(), h, s, ts, o)
			if err != nil || r.Supported != 0 || len(r.Matches) == 0 {
				t.Fatalf("straight chirp passed flyover checks: %+v %v", r.Matches, err)
			}
			for _, m := range r.Matches {
				if m.Fit.Valid && (m.Linear == nil || !m.Linear.Valid) {
					t.Fatal("long recording omitted same-window linear explanation", m)
				}
			}
		})
	}
}

func TestGuidedSelectionCannotReadReservedPower(t *testing.T) {
	s, ts, h, o := recoveryFixture(80, 40, 25, 0)
	c := Config{SoundSpeed: h.SoundSpeed, MaxGap: h.MaxGap}
	p := geo.ECEF(0, 0, 0)
	a, err := guidedFit(context.Background(), c, s, ts[0], p, 1700000000, 20, 60, 0, false, o)
	if err != nil {
		t.Fatal(err)
	}
	for i, row := range s.Rows {
		phase := s.Time(i) - 20
		if phase >= 0 && phase <= 40 && int(math.Floor(phase/10))%2 == 1 {
			for j := range row {
				row[j] = float32(20 * math.Sin(float64(j)))
			}
		}
	}
	b, err := guidedFit(context.Background(), c, s, ts[0], p, 1700000000, 20, 60, 0, false, o)
	if err != nil {
		t.Fatal(err)
	}
	if a.F0 != b.F0 || a.Train != b.Train || a.Selection != b.Selection || a.TrainMargin != b.TrainMargin {
		t.Fatalf("reserved power affected selection: %+v %+v", a, b)
	}
}

func TestGuidedFixedFoldStaysReservedWhenCandidateWindowMoves(t *testing.T) {
	s, ts, h, o := recoveryFixture(80, 40, 25, 0)
	origin := 0.
	o.FoldOrigin = &origin
	c := Config{SoundSpeed: h.SoundSpeed, MaxGap: h.MaxGap}
	p := geo.ECEF(0, 0, 0)
	windows := [][2]float64{{17, 57}, {23.5, 63.5}}
	before := make([]GuidedFit, len(windows))
	for i, w := range windows {
		var err error
		before[i], err = guidedFit(context.Background(), c, s, ts[0], p, 1700000000, w[0], w[1], 0, false, o)
		if err != nil || !before[i].Valid {
			t.Fatal("invalid fixture", err)
		}
	}
	for i, row := range s.Rows {
		if int(math.Floor(s.Time(i)/10))%2 == 1 {
			for j := range row {
				row[j] = float32(30 * math.Cos(float64(j)*.3))
			}
		}
	}
	for i, w := range windows {
		after, err := guidedFit(context.Background(), c, s, ts[0], p, 1700000000, w[0], w[1], 0, false, o)
		if err != nil || after.F0 != before[i].F0 || after.Selection != before[i].Selection || after.Train != before[i].Train {
			t.Fatal("moving the candidate window allowed globally reserved power into training", err)
		}
	}
}
