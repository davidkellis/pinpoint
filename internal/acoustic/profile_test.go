package acoustic

import (
	"math"
	"testing"
)

func shortSpectrum(linear bool) *Spectrum {
	s := &Spectrum{SampleRate: 16384, Window: 8192, Hop: 1024, FirstBin: 50}
	for i := 0; i < 121; i++ {
		tr := s.Time(i) - 4 + 200.0/343
		// Numerical propagation inversion, independent of the fitter formula.
		lo, hi := tr-20, tr
		for k := 0; k < 60; k++ {
			te := (lo + hi) / 2
			if te+math.Hypot(200, 90*te)/343 > tr {
				hi = te
			} else {
				lo = te
			}
		}
		te := (lo + hi) / 2
		hz := 2100 / (1 + 90*90*te/math.Hypot(200, 90*te)/343)
		if linear {
			hz = 2600 - 125*s.Time(i)
		}
		row := make([]float32, 3201)
		for bin := range row {
			for harmonic := 1; harmonic <= 2; harmonic++ {
				delta := (s.Hz(bin) - hz*float64(harmonic)) / 3
				row[bin] += float32(30 * math.Exp(-delta*delta))
			}
		}
		s.Rows = append(s.Rows, row)
	}
	return s
}

func TestShortPassFindsFastHighFrequencyCurveWithoutChangingDefault(t *testing.T) {
	s := shortSpectrum(false)
	if len(s.Discover()) != 0 {
		t.Fatal("short-pass became the default")
	}
	d, err := s.DiscoverProfile(ShortPassProfile)
	if err != nil || d.Strong == 0 {
		t.Fatalf("missed high-frequency physical sweep: err=%v report=%+v", err, d)
	}
	if s.discoveryProfile != "" || len(s.Discover()) != 0 {
		t.Fatal("profile mutated the shared spectrum")
	}
	for _, e := range d.Events {
		if e.Evidence.Level == "strong" && len(e.Ridge) < 30 {
			t.Fatal("fast curve lost time resolution on export")
		}
	}
}

func TestShortPassRejectsLinearPitchDespiteHarmonics(t *testing.T) {
	d, err := shortSpectrum(true).DiscoverProfile(ShortPassProfile)
	if err != nil || d.Strong != 0 || d.Joint != 0 {
		t.Fatalf("generic pitch sweep was promoted: %+v %v", d, err)
	}
	if _, err = shortSpectrum(true).DiscoverProfile("typo"); err == nil {
		t.Fatal("unknown profile accepted")
	}
	s := shortSpectrum(true)
	s.Window *= 4
	if _, err = s.DiscoverProfile(ShortPassProfile); err == nil {
		t.Fatal("long FFT accepted for short-pass")
	}
}
