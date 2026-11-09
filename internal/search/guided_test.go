package search

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pinpoint/internal/acoustic"
	"pinpoint/internal/tracks"
)

func guidedFixture(t *testing.T) (Config, *acoustic.Spectrum, []tracks.Track, []Candidate, GuidedOptions) {
	t.Helper()
	c, s, ts, truth, mirror := timingFixture()
	c.Staged = false
	c.Events = c.Events[:1]
	for len(s.Rows) < 800 {
		s.Rows = append(s.Rows, make([]float32, len(s.Rows[0])))
	}
	var seeds []Candidate
	for _, p := range []observer{truth, mirror} {
		seed, _, ok := evaluatePoint(context.Background(), p, c, s, ts, 1700000000)
		if !ok {
			t.Fatal("lost anchor")
		}
		seeds = append(seeds, seed)
	}
	o := DefaultGuidedOptions()
	o.MinHz = 80
	o.MaxHz = 200
	o.StepHz = .5
	o.Workers = 2
	return c, s, ts, seeds, o
}

func TestGuidedDiscoveryUsesUnusedAudioAtFrozenLocationAndClock(t *testing.T) {
	c, s, ts, seeds, o := guidedFixture(t)
	r, err := Corroborate(context.Background(), c, s, ts, seeds, o, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Candidates) != 2 || r.Candidates[0].Supported != 1 || r.Candidates[1].Supported != 0 {
		t.Fatalf("did not distinguish mirror hypotheses with unseen sound: %+v", r.Candidates)
	}
	best := r.Candidates[0]
	if best.Seed.Lat != seeds[0].Lat || best.Seed.Lon != seeds[0].Lon || best.Seed.ClockOffset != seeds[0].ClockOffset {
		t.Fatal("moved receiver or clock to rescue second sound")
	}
	match := best.Matches[0]
	if match.Track != "east-west" || match.Start <= c.Events[0].End || len(match.Controls) < 3 || match.Fit.Heldout < o.MinContrast || math.Abs(match.AnchorGap-(match.CPA-(best.Seed.Events[0].CPA.Heard-1700000000-best.Seed.ClockOffset))) > 1e-8 {
		t.Fatalf("lost independent window/shared timing: %+v", match)
	}
	dir := t.TempDir()
	if err = WriteGuidedReport(dir, r, s); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "guided.html"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "#ZgotmplZ") || !strings.Contains(string(b), "data:image/png;base64,") {
		t.Fatal("missing measured spectral image")
	}
}

func TestGuidedDiscoveryDoesNotInventEvidenceFromPredictedFlight(t *testing.T) {
	for _, mode := range []string{"missing_sound", "steady_hum", "wrong_spacing"} {
		t.Run(mode, func(t *testing.T) {
			c, s, ts, seeds, o := guidedFixture(t)
			if mode == "wrong_spacing" {
				for i := range ts[1].Points {
					ts[1].Points[i].Time += 20
				}
			} else {
				for frame, row := range s.Rows {
					if s.Time(frame) <= c.Events[0].End+2 {
						continue
					}
					clear(row)
					if mode == "steady_hum" {
						row[110] = 20
					}
				}
			}
			r, err := Corroborate(context.Background(), c, s, ts, seeds, o, nil)
			if err != nil {
				t.Fatal(err)
			}
			for _, p := range r.Candidates {
				if !p.AnchorValid || p.Supported != 0 {
					t.Fatalf("fabricated corroboration for %s: %+v", mode, p)
				}
			}
		})
	}
}

func TestGuidedFlyoverSurvivesLouderUnrelatedSteadyHum(t *testing.T) {
	c, s, ts, seeds, o := guidedFixture(t)
	for frame, row := range s.Rows {
		if s.Time(frame) > c.Events[0].End+2 {
			// This narrow 90 Hz tone is outside the second flyover's band.
			row[50] = 60
		}
	}
	r, err := Corroborate(context.Background(), c, s, ts, seeds, o, nil)
	if err != nil {
		t.Fatal(err)
	}
	if r.Candidates[0].Supported != 1 || r.Candidates[0].Seed.Lon != seeds[0].Lon {
		t.Fatal("unrelated hum vetoed the real curved sound")
	}
}

func TestGuidedSearchRefusesStaleSeedsAndCancels(t *testing.T) {
	c, s, ts, seeds, o := guidedFixture(t)
	for i := range ts[0].Points {
		ts[0].Points[i].Time += 1000
	}
	r, err := Corroborate(context.Background(), c, s, ts, seeds, o, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range r.Candidates {
		if p.AnchorValid || len(p.Matches) > 0 {
			t.Fatal("used stale seed instead of rechecking its anchor")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = Corroborate(ctx, c, s, ts, seeds, o, nil); err != context.Canceled {
		t.Fatalf("ignored cancellation: %v", err)
	}
}

func TestGuidedDoesNotCountSegmentsOrOverlappingSoundAsNewAircraft(t *testing.T) {
	for _, mode := range []string{"same_as_anchor", "duplicate_segments", "overlapping_aircraft"} {
		t.Run(mode, func(t *testing.T) {
			c, s, ts, seeds, o := guidedFixture(t)
			ts[0].ID = "abc123-1700000000.000-1"
			for i := range seeds {
				seeds[i].Events[0].Track = ts[0].ID
			}
			if mode == "same_as_anchor" {
				ts[1].ID = "ABC123-1700000000.000-2"
			} else {
				ts[1].ID = "def456-1700000000.000-1"
				duplicate := ts[1]
				duplicate.ID = "def456-1700000000.000-2"
				if mode == "overlapping_aircraft" {
					duplicate.ID = "fed321-1700000000.000-1"
				}
				ts = append(ts, duplicate)
			}
			r, err := Corroborate(context.Background(), c, s, ts, seeds, o, nil)
			if err != nil {
				t.Fatal(err)
			}
			want := 1
			if mode == "same_as_anchor" {
				want = 0
			}
			if r.Candidates[0].Supported != want {
				t.Fatalf("counted nonindependent evidence: %+v", r.Candidates[0])
			}
		})
	}
}
