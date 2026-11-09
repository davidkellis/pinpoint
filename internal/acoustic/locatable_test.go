package acoustic

import "testing"

func TestLocatableCurvesKeepsStrongPlausibleCurvesInTimeOrder(t *testing.T) {
	ridge := func(hz float64, n int) []RidgeSample {
		r := make([]RidgeSample, n)
		for i := range r {
			r[i] = RidgeSample{Time: float64(i), Hz: hz}
		}
		return r
	}
	strong := &CurveEvidence{Level: "strong"}
	events := []Event{
		{Start: 50, End: 170, Evidence: strong, Ridge: ridge(120, 40)},                        // kept
		{Start: 10, End: 30, Evidence: strong, Ridge: ridge(6800, 40)},                        // too high to carry kilometres
		{Start: 0, End: 100, Evidence: &CurveEvidence{Level: "review"}, Ridge: ridge(90, 40)}, // not strong
		{Start: 5, End: 40, Evidence: strong, Ridge: ridge(400, 20)},                          // kept, earlier
		{Start: 200, End: 210, Evidence: strong, Ridge: ridge(300, 5)},                        // too few samples
		{Start: 300, End: 305, Evidence: strong, Ridge: ridge(900, 12)},                       // kept unless limited
	}
	got, info := LocatableCurves(events, DefaultLocatableMaxHz, 0)
	if len(got) != 3 || info[0].Index != 4 || info[1].Index != 1 || info[2].Index != 6 || info[1].MedianHz != 120 {
		t.Fatal("wrong selection or order", info)
	}
	got, info = LocatableCurves(events, DefaultLocatableMaxHz, 2)
	if len(got) != 2 || info[0].Index != 4 || info[1].Index != 1 {
		t.Fatal("limit must keep the longest curves, then restore time order", info)
	}
}
