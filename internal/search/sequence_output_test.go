package search

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSequenceOverviewDisplaysMissingControlHistory(t *testing.T) {
	r := SequenceReport{Status: "incomplete", Options: SequenceOptions{ControlShifts: []float64{-600, 600}}, Groups: []SequenceGroup{
		{ID: 0, Indices: []int{0, 1}, Status: "no_training_match"},
		{ID: 1, Trial: 1, Shift: -600, Status: "error", Reasons: []Rejection{{Code: "telemetry_window", Detail: "Historical telemetry does not cover the negative time control."}}},
	}}
	dir := t.TempDir()
	if err := WriteSequenceReport(dir, r, func(int) string { return "" }); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "automatic.html"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"incomplete", "Incorrect-time controls", "telemetry_window", "Historical telemetry does not cover", "Not searched"} {
		if !strings.Contains(string(b), want) {
			t.Fatal("control failure was hidden from the overview", want)
		}
	}
}
