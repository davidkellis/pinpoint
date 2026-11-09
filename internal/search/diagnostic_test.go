package search

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"pinpoint/internal/acoustic"
	"pinpoint/internal/geo"
	"strings"
	"testing"
)

func TestDiagnosticScorerParityAndFailures(t *testing.T) {
	c, s, ts := scoreGridFixture()
	e := c.Events[0]
	p := geo.ECEF(0, 0, 0)
	for frame, row := range s.Rows {
		factor, _, ok := ts[0].Received(p, 1700000000+s.Time(frame), 343, 30)
		if !ok {
			continue
		}
		hz := 100 * factor
		for k := range row {
			d := s.Hz(k) - hz
			row[k] = float32(20 * math.Exp(-d*d))
		}
		if s.Time(frame) >= e.Start && s.Time(frame) <= e.End {
			e.Ridge = append(e.Ridge, acoustic.RidgeSample{Time: s.Time(frame), Hz: hz})
		}
	}
	c.Events = []acoustic.Event{e}
	for _, bad := range []bool{false, true} {
		if bad {
			for k := range e.Ridge {
				e.Ridge[k].Hz += 5 * math.Sin(float64(k))
			}
		}
		c.Events[0] = e
		f, score, cov := scoreTrack(ts[0], p, s, e, 1700000000, 343, 30)
		d := FitDiagnostic{}
		f2, score2, cov2 := scoreObservedTrackDiagnostic(ts[0], p, s, e, 1700000000, 343, 30, prepareObservations(s, e), &d)
		if f != f2 || score != score2 || cov != cov2 {
			t.Fatal("diagnostics changed scoring")
		}
		if bad && (d.Reason != "ridge_residual" || d.RMSE == nil || *d.RMSE <= *d.RMSELimit) {
			t.Fatalf("missing residual rejection: %+v", d)
		}
		if !bad && (d.Reason != "scored" || *d.RMSE > 1e-6 || d.Score == nil || *d.Score != score) {
			t.Fatalf("incorrect successful fit: %+v", d)
		}
		r, err := Diagnose(c, s, ts, 0, 0, 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(r.Fits) != 1 || len(r.Fits[0].Points) != len(e.Ridge) {
			t.Fatal("lost full-resolution residuals")
		}
		if _, err = json.Marshal(r); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := Diagnose(c, s, ts, 0, 0, 1); err == nil {
		t.Fatal("accepted clock outside search window")
	}
	if _, err := Diagnose(c, s, ts, math.NaN(), 0, 0); err == nil {
		t.Fatal("accepted invalid coordinate")
	}
}

func TestDiagnosticExportEscapesLabelsAndShowsNumbers(t *testing.T) {
	value := 3.25
	r := DiagnosticReport{Fits: []FitDiagnostic{{Track: "<script>alert(1)</script>", Event: 1, RMSE: &value, RMSELimit: &value,
		FullResolution: &FitSupport{RMSE: &value, MedianAbsResidual: &value, MedianContrast: &value, PositiveContrastFraction: &value},
		Points:         []FitResidual{{Time: 1, Observed: 10, Predicted: 11, Residual: -1, ImpliedSource: 10}, {Time: 2, Observed: 9, Predicted: 10, Residual: -1, ImpliedSource: 10}},
	}}}
	dir := t.TempDir()
	if err := WriteDiagnostic(dir, r); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "diagnostic.html"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if strings.Contains(s, "<script>") || strings.Contains(s, "%!") || strings.Contains(s, "NaN") || !strings.Contains(s, "3.250") || !strings.Contains(s, "&lt;script&gt;") {
		t.Fatal("unsafe or malformed diagnostic report", s)
	}
	var decoded DiagnosticReport
	b, err = os.ReadFile(filepath.Join(dir, "diagnostic.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(b, &decoded); err != nil || len(decoded.Fits) != 1 {
		t.Fatal("invalid JSON export", err)
	}
	b, err = os.ReadFile(filepath.Join(dir, "residuals.csv"))
	if err != nil || !strings.Contains(string(b), "implied_source_hz") {
		t.Fatal("missing residual export", err)
	}
}

func TestFullResolutionCheckExposesErrorsBetweenCoarseSamples(t *testing.T) {
	c, s, ts := scoreGridFixture()
	s.Hop = 128
	s.Rows = make([][]float32, 321)
	p := geo.ECEF(0, 0, 0)
	e := c.Events[0]
	first := int(math.Ceil((e.Start - s.Time(0)) / s.Step()))
	for frame := range s.Rows {
		s.Rows[frame] = make([]float32, 200)
		for k := range s.Rows[frame] {
			s.Rows[frame][k] = 8
		}
		time := s.Time(frame)
		factor, _, ok := ts[0].Received(p, 1700000000+time, 343, 30)
		if !ok || time < e.Start || time > e.End {
			continue
		}
		hz := 100 * factor
		if (frame-first)%8 != 0 {
			hz += 5
		}
		e.Ridge = append(e.Ridge, acoustic.RidgeSample{Time: time, Hz: hz})
	}
	c.Events = []acoustic.Event{e}
	r, err := Diagnose(c, s, ts, 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	d := r.Fits[0]
	if d.Reason != "scored" || *d.RMSE > 1e-6 {
		t.Fatal("fixture did not isolate the one-Hz sampling limitation")
	}
	if d.FullResolution == nil || *d.FullResolution.RMSE < 4 || d.FullResolution.RidgeSamples <= d.RidgeSamples {
		t.Fatal("dense diagnostic missed errors between scorer samples")
	}
	if *d.FullResolution.MedianContrast != 0 || *d.FullResolution.PositiveContrastFraction != 0 {
		t.Fatal("flat background incorrectly supports a spectral ridge")
	}
}
