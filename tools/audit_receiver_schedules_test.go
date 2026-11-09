//go:build ignore

package main

import (
	"testing"

	"pinpoint/internal/search"
)

func TestScheduleSelectionIsTrainingOnlyAndRejectsDuplicateAircraft(t *testing.T) {
	makeMatch := func(id string, cpa, train, heldout float64) search.GuidedMatch {
		return search.GuidedMatch{Track: id, CPA: cpa, Start: cpa - 20, End: cpa + 20, Fit: search.GuidedFit{Train: train, Heldout: heldout, Valid: true}, Flat: search.GuidedFit{Valid: true}, Supported: heldout > 3}
	}
	a := makeMatch("abc123-1700000000.000-1", 500, 10, -3)
	b := makeMatch("abc123-1700000000.000-2", 600, 9, 20)
	c := makeMatch("def456-1700000000.000-1", 800, 8, 15)
	cohort := map[string]bool{cohortKey(a, 0): true, cohortKey(b, 0): true, cohortKey(c, 0): true}
	s := chooseSchedule(search.ReceiverReview{Matches: []search.GuidedMatch{a, b, c}}, 0, cohort, 2)
	if !s.Valid || s.Quality || len(s.Events) != 2 || s.Events[0].Track != a.Track || s.Events[1].Track != c.Track {
		t.Fatal("heldout rescue or duplicate aircraft in schedule", s)
	}
	shifted := a
	shifted.CPA -= 140
	if cohortKey(shifted, 140) != cohortKey(a, 0) {
		t.Fatal("null clock changed encounter identity")
	}
}
