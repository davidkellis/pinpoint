package mapview

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPortableMapEscapesEmbeddedData(t *testing.T) {
	name := `x</script><script>alert("bad")</script>&` + "\u2028"
	path := filepath.Join(t.TempDir(), "map.html")
	if err := Write(path, map[string]string{"source": name}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	page := string(b)
	if strings.Contains(page, name) || strings.Contains(page, "__MAP_DATA__") || strings.Contains(page, "__APP_JS__") || strings.Contains(page, `<script src=`) {
		t.Fatal("unsafe, incomplete or network-dependent application code")
	}
	const start = `<script id="map-data" type="application/json">`
	_, rest, ok := strings.Cut(page, start)
	if !ok {
		t.Fatal("missing data block")
	}
	payload, _, ok := strings.Cut(rest, "</script>")
	if !ok {
		t.Fatal("missing end of data block")
	}
	var decoded map[string]string
	if err = json.Unmarshal([]byte(payload), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["source"] != name {
		t.Fatal("source did not round-trip")
	}
	if !strings.Contains(page, "BSD 2-Clause License") || !strings.Contains(page, "Leaflet 1.9.4") {
		t.Fatal("missing bundled library license")
	}
}

func TestMapRejectsNonfiniteScores(t *testing.T) {
	if err := Write(filepath.Join(t.TempDir(), "map.html"), map[string]float64{"score": math.NaN()}); err == nil {
		t.Fatal("accepted NaN map data")
	}
}

func TestSavedMapsReceiveCurrentControlsWithoutChangingEvidence(t *testing.T) {
	original := `{"source":"saved audio","report":{"score_grid":[{"lat":29.5,"score_db":7.3}],"note":"known-location validation"}}`
	old := []byte(`<html><script id="map-data" type="application/json">` + original + `</script><script>old controls</script></html>`)
	page, err := Refresh(old)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(page), "state-borders") || !strings.Contains(string(page), "compare-curves") || strings.Contains(string(page), "old controls") {
		t.Fatal("saved view was not upgraded")
	}
	_, rest, _ := strings.Cut(string(page), `<script id="map-data" type="application/json">`)
	payload, _, _ := strings.Cut(rest, "</script>")
	if payload != original {
		t.Fatalf("saved scores/evidence changed: %s", payload)
	}
	if _, err = Refresh([]byte("broken saved map")); err == nil {
		t.Fatal("accepted missing data")
	}
}
