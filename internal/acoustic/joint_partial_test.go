package acoustic

import "testing"

func TestJointSelectionRetainsStrongPartialTimingWithoutInventingCPA(t *testing.T) {
	m := &RidgeMeasurement{ShapePass: true, Usable: false, CPA: 1000, CPATolerance: 100}
	e := Event{Start: 10, End: 30, CPATime: 20, Partial: true, Measurement: m, Evidence: &CurveEvidence{Level: "strong", Predictive: true}, FMin: 50, FMax: 250, MinSpeed: 15, MaxSpeed: 300, MaxSlant: 15000}
	for i := 0; i < 10; i++ {
		e.Ridge = append(e.Ridge, RidgeSample{Time: 10 + float64(i)*2, Hz: 130 - float64(i)})
	}
	for _, weak := range []bool{false, true} {
		got := SearchEvents([]Event{e}, true, weak)
		if len(got) != 1 || !got[0].Partial || got[0].CPATolerance != 0 || len(got[0].Ridge) != 10 {
			t.Fatalf("lost partial measured timing: %+v", got)
		}
		if got[0].FMin != e.FMin || got[0].MaxSlant != e.MaxSlant || m.Usable {
			t.Fatal("invented certainty from partial fit")
		}
	}
	for _, level := range []string{"weak", "review"} {
		copy := e
		copy.Evidence = &CurveEvidence{Level: level, Predictive: true}
		if len(SearchEvents([]Event{copy}, true, true)) != 0 {
			t.Fatal("promoted weak/review curve because joint timing is available")
		}
	}
	e.Evidence.Predictive = false
	if len(SearchEvents([]Event{e}, true, false)) != 0 {
		t.Fatal("accepted nonpredictive partial curve")
	}
}
