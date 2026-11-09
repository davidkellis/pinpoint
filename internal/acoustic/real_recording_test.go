package acoustic

import (
	"compress/gzip"
	"encoding/gob"
	"fmt"
	"math"
	"os"
	"testing"
)

func loadRealSpectrum(t *testing.T, name string) *Spectrum {
	t.Helper()
	f, err := os.Open("testdata/" + name + ".gob.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	g, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	var s Spectrum
	if err = gob.NewDecoder(g).Decode(&s); err != nil {
		t.Fatal(err)
	}
	return &s
}

// Frequencies were read from the real spectrogram, independently of the
// detector. Counting arbitrary "strong" fragments would not catch the v7 bug.
func TestRealReferenceCurves(t *testing.T) {
	cases := []struct {
		number      int
		offset, cpa float64
		anchors     []RidgeSample
	}{
		{1, 2000, 2156, []RidgeSample{{2128, 144}, {2140, 131.5}, {2154, 114}, {2168, 102}, {2200, 89}}},
		{2, 2940, 3077, []RidgeSample{{3050, 168}, {3060, 155.5}, {3070, 138}, {3080, 123.5}, {3090, 116}, {3100, 110}}},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprint(tc.number), func(t *testing.T) {
			a, b := loadRealSpectrum(t, fmt.Sprintf("colsto-%d-long", tc.number)), loadRealSpectrum(t, fmt.Sprintf("colsto-%d-short", tc.number))
			report := CombineDetections(a, CorroborateCurves(a.DiscoverAll(), b), CorroborateCurves(b.DiscoverAll(), a))
			matches := 0
			for _, e := range SearchEvents(report.Events, true, false) {
				good := true
				for _, anchor := range tc.anchors {
					hz, ok := e.RidgeHz(anchor.Time - tc.offset)
					if !ok || math.Abs(hz-anchor.Hz) > 3 {
						good = false
						break
					}
				}
				if !good {
					continue
				}
				if e.End-e.Start < 80 || math.Abs(e.Measurement.CPA+tc.offset-tc.cpa) > 10 {
					t.Fatalf("known curve fragmented or CPA biased: %+v", e.Measurement)
				}
				matches++
				t.Logf("known flyover: %.1f–%.1fs, arrival CPA %.1fs, %.1fHz source", e.Start+tc.offset, e.End+tc.offset, e.Measurement.CPA+tc.offset, e.Measurement.F0)
			}
			if matches != 1 {
				t.Fatalf("wanted one joined, jointly usable path through the known spectral anchors; got %d (%d strong candidates)", matches, report.Strong)
			}
		})
	}
}
