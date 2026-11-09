package search

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"pinpoint/internal/geo"
	"pinpoint/internal/tracks"
	"testing"
)

func TestAssignmentMatchesExhaustiveOracle(t *testing.T) {
	rng := rand.New(rand.NewSource(17))
	for run := 0; run < 100; run++ {
		choices := make([][]Match, 5)
		for i := range choices {
			for j := 0; j < 7; j++ {
				if rng.Intn(3) > 0 {
					choices[i] = append(choices[i], Match{Track: fmt.Sprint(j), Score: float64(rng.Intn(20))})
				}
			}
		}
		best := math.Inf(-1)
		var visit func(int, float64, map[string]bool)
		visit = func(i int, sum float64, used map[string]bool) {
			if i == len(choices) {
				best = math.Max(best, sum)
				return
			}
			for _, m := range choices[i] {
				if used[m.Track] || m.Score < 5 {
					continue
				}
				used[m.Track] = true
				visit(i+1, sum+m.Score, used)
				delete(used, m.Track)
			}
		}
		visit(0, 0, map[string]bool{})
		got := distinctMatches(choices, 5)
		sum := math.Inf(-1)
		if len(got) > 0 {
			sum = 0
			for _, m := range got {
				sum += m.Score
			}
		}
		if sum != best {
			t.Fatalf("assignment score %g, exhaustive optimum %g", sum, best)
		}
	}
	choices := make([][]Match, 32)
	for i := range choices {
		for j := 0; j < 32; j++ {
			choices[i] = append(choices[i], Match{Track: fmt.Sprint(j), Score: 10})
		}
	}
	if len(distinctMatches(choices, 5)) != 32 {
		t.Fatal("lost usable events above old cap")
	}
}

func TestSilenceRejectsFlightWhoseCPAIsOutsideSilentInterval(t *testing.T) {
	p := observer{pos: geo.Vec{}}
	tr := tracks.Track{Points: []tracks.Point{{Time: 0, Position: geo.Vec{X: -1000, Y: 300}}, {Time: 20, Position: geo.Vec{X: 1000, Y: 300}}}}
	c := DefaultConfig()
	c.Silence = []SilentInterval{{Start: 8, End: 9, MaxSlant: 500}}
	// Heard CPA is at 10.875 s, outside this interval. Sound is already audible.
	if silenceAllows(c, []tracks.Track{tr}, p, 0) {
		t.Fatal("CPA-only test missed audible approach")
	}
	if !silenceAllows(c, []tracks.Track{tr}, p, 20) {
		t.Fatal("silence ignored shared recording clock")
	}
}

func TestProgressiveRefinementKeepsCompetingFlightsAndRescoresAllEvents(t *testing.T) {
	p, c, s, ts, _ := benchmarkPoint()
	c.ClockUncertainty = 0
	c.Grid = 100
	c.Refine = 50
	c.FinalGrid = 10
	c.Bounds = geo.Bounds{South: -.001, North: .001, West: -.001, East: .001}
	c.MinScore = 1
	r, err := Run(context.Background(), c, s, ts)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.RefinementPasses) != 4 || r.FinalGrid != 10 {
		t.Fatalf("missing progressive refinement: %+v", r.RefinementPasses)
	}
	for i, w := range r.RefinementPasses {
		if w.Locations == 0 || i > 0 && w.Spacing >= r.RefinementPasses[i-1].Spacing {
			t.Fatalf("empty or non-descending pass %+v", w)
		}
	}
	if len(r.Candidates) == 0 {
		t.Fatal("lost physical candidate during refinement")
	}
	if geo.ECEF(r.Candidates[0].Lat, r.Candidates[0].Lon, 0).Sub(p.pos).Norm() > 100 {
		t.Fatal("refinement failed around physical origin")
	}
	var hypotheses []Candidate
	for i := 0; i < 20; i++ {
		hypotheses = append(hypotheses, Candidate{Lat: float64(i) * .01, Events: []Match{{Track: "A"}}})
	}
	hypotheses = append(hypotheses, Candidate{Lat: 1, Events: []Match{{Track: "B"}}})
	seeds := refinementSeeds(hypotheses, 100, 8)
	if seeds[1].Events[0].Track != "B" {
		t.Fatal("dominant flight pair monopolized fine search")
	}
}

func TestFineFitKeepsOriginalTimeAndFrequencyLimits(t *testing.T) {
	_, c, _, _, _ := benchmarkPoint()
	c.ClockUncertainty = 1
	c.ClockStep = .5
	c.Events[0].Ridge = nil
	c.Events[0].FMin = 129
	c.Events[0].FMax = 131
	seed := Candidate{ClockOffset: .9, Events: []Match{{Track: "A", F0: 130}}}
	fine := refinementConfig(c, seed, 25)
	if fine.eventTrackIDs[0] != "A" || fine.Events[0].FStep > .05 {
		t.Fatal("fine pass did not lock the surviving flight and refine its tone")
	}
	for _, offset := range fine.clockOffsets {
		if math.Abs(offset) > 1 {
			t.Fatal("fine pass exceeded original clock bounds")
		}
	}
	if fine.Events[0].FMin < 129 || fine.Events[0].FMax > 131 {
		t.Fatal("fine pass exceeded measured frequency bounds")
	}
	if c.Events[0].FStep != .5 {
		t.Fatal("refinement mutated shared event inputs")
	}
}
