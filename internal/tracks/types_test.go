package tracks

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestAircraftTypesSurviveImportAndExcludeOnlyKnownJets(t *testing.T) {
	p := filepath.Join(t.TempDir(), "trace.json")
	data := `{"icao":"abc123","t":"T38","timestamp":1000,"trace":[[0,35,-100,1000,100,90,8,0],[1,35,-99.999,1000,100,90,8,0]]}`
	if err := os.WriteFile(p, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	var csv bytes.Buffer
	if _, err := Import(p, &csv, ImportOptions{Start: 1000, End: 1002, MaxGap: 15}); err != nil {
		t.Fatal(err)
	}
	ts, err := Read(&csv)
	if err != nil || len(ts) != 1 || ts[0].TypeCode != "T38" {
		t.Fatalf("type lost: %+v %v", ts, err)
	}
	ts = append(ts, Track{ID: "prop", TypeCode: "C172", Points: ts[0].Points}, Track{ID: "turboprop", TypeCode: "PC12", Points: ts[0].Points}, Track{ID: "unknown", TypeCode: "FUTURE", Points: ts[0].Points}, Track{ID: "missing", Points: ts[0].Points})
	if err := Write(&csv, ts); err != nil {
		t.Fatal(err)
	}
	ts, err = Read(&csv)
	if err != nil {
		t.Fatal(err)
	}
	kept, stats := ExcludeKnownJets(ts)
	if len(kept) != 4 || stats.ExcludedJets != 1 || stats.KnownNonJets != 2 || stats.Unknown != 2 {
		t.Fatalf("bad filter: %+v", stats)
	}
}
