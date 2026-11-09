package curvebench

import (
	"context"
	"math"
	"reflect"
	"testing"
)

func TestSelectedProductionMethodRunsBothPasses(t *testing.T) {
	c := Case{ID: "injection", Label: "fixture", Kind: "injection", Duration: 80, Seed: 32, Inject: &Injection{Kind: "prop", SNRDB: 5}}
	full, err := Run(context.Background(), Config{Cases: []Case{c}, Methods: []string{"stft-2s", "stft-4s", "production-2+4s"}}, t.TempDir(), 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	d := c
	d.ID = "duplicate"
	out := t.TempDir()
	selected, err := Run(context.Background(), Config{Cases: []Case{c, d}, Methods: []string{"production-2+4s"}}, out, 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := full.Reports[0].Results[2]
	if want.Fragments == 0 {
		t.Fatal("empty fixture cannot verify selected baseline")
	}
	for _, r := range selected.Reports {
		got := r.Results[0]
		if got.Fragments != want.Fragments || !reflect.DeepEqual(got.Events, want.Events) || !reflect.DeepEqual(got.Match, want.Match) {
			t.Fatal("combined detection depends on selected method order or worker count")
		}
	}
	if err := Rebuild(out); err != nil {
		t.Fatal(err)
	}
}

func TestInvalidExperimentRejectedBeforeWork(t *testing.T) {
	c := Case{ID: "input", Kind: "injection", Duration: 80, Inject: &Injection{Kind: "jet", SNRDB: math.NaN()}}
	if Validate(Config{Cases: []Case{c}}) == nil {
		t.Fatal("accepted NaN signal strength")
	}
	c.Inject = nil
	for _, methods := range [][]string{{"unknown"}, {"stft-2s", "stft-2s"}} {
		if Validate(Config{Cases: []Case{c}, Methods: methods}) == nil {
			t.Fatal("accepted invalid methods")
		}
	}
}
