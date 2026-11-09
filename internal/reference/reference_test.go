package reference

import (
	"os"
	"path/filepath"
	"testing"

	"pinpoint/internal/acoustic"
	"pinpoint/internal/geo"
	"pinpoint/internal/search"
)

func label() Label {
	return Label{ID: "plane", Anchors: []acoustic.RidgeSample{{Time: 10, Hz: 150}, {Time: 20, Hz: 130}, {Time: 30, Hz: 110}}, MinHit: 1, MinCoverage: 1, MaxRMSE: 3, RequireStrong: true}
}
func event(points []acoustic.RidgeSample) acoustic.Event {
	return acoustic.Event{Start: points[0].Time, End: points[len(points)-1].Time, Ridge: points, Evidence: &acoustic.CurveEvidence{Level: "strong"}}
}

func TestSinglePassageSelectionKeepsQualityAndDefaultMultiEventPolicy(t *testing.T) {
	makeEvent := func(start, end float64, level string, usable bool) acoustic.Event {
		return acoustic.Event{Start: start, End: end,
			Evidence:    &acoustic.CurveEvidence{Level: level},
			Measurement: &acoustic.RidgeMeasurement{Usable: usable}}
	}
	events := []acoustic.Event{
		makeEvent(5, 200, "weak", true), // longest sound is not eligible
		makeEvent(10, 100, "strong", false),
		makeEvent(70, 110, "strong", true),
		makeEvent(30, 70, "strong", true), // tie resolved by earlier start
	}
	got := locationEvents(events, "longest_usable")
	if len(got) != 1 || got[0].Start != 30 {
		t.Fatalf("selection bypassed quality or temporal tie-break: %+v", got)
	}
	if len(locationEvents(events, "")) != 2 {
		t.Fatal("default policy must retain all independently usable events")
	}
	if len(locationEvents(events[:2], "longest_usable")) != 0 {
		t.Fatal("absence of strong usable curves must not fall back to weak evidence")
	}
	if events[3].CPATime != 0 {
		t.Fatal("selection mutated input detections")
	}
}
func TestAnchorsRequireOneCorrectRidge(t *testing.T) {
	l := label()
	e := event(l.Anchors)
	if !Match([]acoustic.Event{e}, l, 0).Pass {
		t.Fatal("known curve rejected")
	}
	wrong := event([]acoustic.RidgeSample{{Time: 10, Hz: 200}, {Time: 20, Hz: 200}, {Time: 30, Hz: 200}})
	if Match([]acoustic.Event{wrong}, l, 0).Pass {
		t.Fatal("unrelated strong tone passed")
	}
	parts := []acoustic.Event{event(l.Anchors[:2]), event(l.Anchors[1:])}
	if Match(parts, l, 0).Pass {
		t.Fatal("stitched fragmented paths passed")
	}
	e.Evidence.Level = "review"
	if Match([]acoustic.Event{e}, l, 0).Pass {
		t.Fatal("weak evidence passed")
	}
	e = event([]acoustic.RidgeSample{{Time: 10, Hz: 300}, {Time: 20, Hz: 260}, {Time: 30, Hz: 220}})
	if r := Match([]acoustic.Event{e}, l, 0); !r.Pass || r.Ratio != 2 {
		t.Fatalf("consistent harmonic rejected: %+v", r)
	}
	if Match([]acoustic.Event{e}, l, 9).Pass {
		t.Fatal("shifted curve passed")
	}
}
func TestAssetIntegrityAndEmptySuite(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "audio.wav")
	if err := os.WriteFile(p, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	h, _ := hashFile(p)
	s := Suite{Version: 1, Assets: []Asset{{Path: "audio.wav", SHA256: h, Profiles: []string{"quick"}}}, Acoustic: []AcousticCase{{ID: "one", Profiles: []string{"quick"}, Audio: "audio.wav", Labels: []Label{label()}}}}
	if n, e := Verify(dir, s, "quick"); e != nil || n != 1 {
		t.Fatalf("%d %v", n, e)
	}
	if _, e := Verify(dir, s, "typo"); e == nil {
		t.Fatal("unknown profile passed")
	}
	if _, e := Verify(dir, Suite{Version: 1}, "quick"); e == nil {
		t.Fatal("empty suite passed")
	}
	s.Acoustic[0].Labels = nil
	if _, e := Verify(dir, s, "quick"); e == nil {
		t.Fatal("unlabeled-only suite passed")
	}
	s.Acoustic[0].Labels = []Label{label()}
	os.WriteFile(p, []byte("changed"), 0600)
	if _, e := Verify(dir, s, "quick"); e == nil {
		t.Fatal("changed audio passed")
	}
	os.Remove(p)
	if _, e := Verify(dir, s, "quick"); e == nil {
		t.Fatal("missing audio passed")
	}
}
func TestLocationChecksIdentityRegionAndAmbiguity(t *testing.T) {
	c := LocationCase{ID: "test", ExpectedAircraft: []string{"aaa077"}, ExpectedRegion: geo.Bounds{South: 37.5, North: 37.85, West: -122, East: -121.6}, RequireAmbiguity: true}
	g := []search.Candidate{{Lat: 37.65, Lon: -121.79, Score: 19, Events: []search.Match{{Track: "aaa077-segment"}}}, {Lat: 39, Lon: -121.1, Score: 18.75, Events: []search.Match{{Track: "wrong-segment"}}}}
	if r := EvaluateLocation(c, g); r.Status != "pass" || r.Ambiguity == "" {
		t.Fatalf("%+v", r)
	}
	if EvaluateLocation(c, g[:1]).Status != "fail" {
		t.Fatal("lost competing flight passed")
	}
	g[0].Events[0].Track = "wrong-segment"
	if EvaluateLocation(c, g).Status != "fail" {
		t.Fatal("wrong identity passed")
	}
	g[0].Events[0].Track = "aaa077-segment"
	g[0].Lat = 36
	if EvaluateLocation(c, g).Status != "fail" {
		t.Fatal("wrong region passed")
	}
	if EvaluateLocation(c, nil).Status != "fail" {
		t.Fatal("empty search passed")
	}
}

func TestNegativeControlCannotPassWithStrongCandidate(t *testing.T) {
	zero := 0
	c := AcousticCase{ID: "background", MaxStrong: &zero}
	if r := evaluateAcoustic(c, CaseResult{Status: "pass"}, acoustic.DetectionReport{}); r.Status != "pass" {
		t.Fatalf("clean negative failed: %+v", r)
	}
	if r := evaluateAcoustic(c, CaseResult{Status: "pass"}, acoustic.DetectionReport{Strong: 1}); r.Status != "fail" {
		t.Fatal("false-positive negative control passed")
	}
	c.MaxStrong = nil
	if r := evaluateAcoustic(c, CaseResult{Status: "pass"}, acoustic.DetectionReport{}); r.Status != "unlabeled_observation" {
		t.Fatal("missing labels counted as a recognition pass")
	}
}

func TestKnownLocationSupportIsNotConfusedWithWinningLocationAccuracy(t *testing.T) {
	c := LocationCase{ID: "private-evaluation", ExpectedAircraft: []string{"abc123"},
		ExpectedRegion: geo.Bounds{South: 35, North: 36, West: -101, East: -100}, RequireAmbiguity: true,
		KnownLocation: &LocationLabel{Lat: 35.54, Lon: -100.35, SupportRadius: 250, MaxScoreGap: 1}}
	g := []search.Candidate{{Lat: 35.4, Lon: -100.2, Score: 15.5, Events: []search.Match{{Track: "abc123-flight"}}}}
	score := 14.9
	grid := []search.ScorePoint{{Lat: 35.5401, Lon: -100.35, Score: &score, MeetsThreshold: true, TrackIDs: []string{"abc123-flight"}}}
	r := EvaluateLocation(c, g, grid)
	if r.Status != "pass" || r.KnownLocation == nil || r.KnownLocation.BestError < 10000 || r.Ambiguity == "" {
		t.Fatalf("should retain nearby support AND report the inaccurate winning position: %+v", r)
	}
	grid[0].TrackIDs[0] = "unrelated-flight"
	if EvaluateLocation(c, g, grid).Status != "fail" {
		t.Fatal("unrelated aircraft near the reference point counted as support")
	}
	if EvaluateLocation(c, g).Status != "fail" {
		t.Fatal("missing location surface passed the support check")
	}
}
