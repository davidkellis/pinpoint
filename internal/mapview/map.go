// Package mapview writes a portable map report. Code, styles and search data
// are embedded; satellite tiles are fetched by the browser when viewed.
package mapview

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"pinpoint/internal/geo"
	"strings"
)

//go:embed assets/*
var assets embed.FS

func LibraryAsset(name string) ([]byte, error) {
	if name == "state-borders.json" {
		return geo.StateBorders()
	}
	if name != "leaflet.js" && name != "leaflet.css" && name != "map-context.js" && name != "evidence.js" && name != "evidence.css" {
		return nil, os.ErrNotExist
	}
	return assets.ReadFile("assets/" + name)
}

func Write(path string, data any) error {
	page, err := Render(data)
	if err != nil {
		return err
	}
	return os.WriteFile(path, page, 0644)
}

func Render(data any) ([]byte, error) {
	payload, err := json.Marshal(data) // Escapes HTML, including </script> in names.
	if err != nil {
		return nil, err
	}
	borders, err := geo.StateBorders()
	if err != nil {
		return nil, err
	}
	read := func(name string) string {
		b, err := assets.ReadFile("assets/" + name)
		if err != nil {
			panic(err)
		}
		return string(b)
	}
	page := strings.NewReplacer(
		"__LEAFLET_CSS__", read("leaflet.css"),
		"__LEAFLET_JS__", read("leaflet.js"),
		"__LEAFLET_LICENSE__", read("LEAFLET-LICENSE"),
		"__APP_CSS__", read("map.css"),
		"__APP_JS__", read("map.js"),
		"__EVIDENCE_JS__", read("evidence.js"),
		"__EVIDENCE_CSS__", read("evidence.css"),
		"__MAP_CONTEXT__", read("map-context.js"),
		"__STATE_BORDERS__", string(borders),
		"__MAP_DATA__", string(payload),
	).Replace(read("map.html"))
	return []byte(page), nil
}

// RenderConsensus renders a consensus search (regions, evaluated points and
// offered curves) as a portable page. data must marshal to an object with
// "result", "points" and "curves" fields.
func RenderConsensus(data any) ([]byte, error) {
	payload, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	borders, err := geo.StateBorders()
	if err != nil {
		return nil, err
	}
	read := func(name string) (string, error) {
		b, err := assets.ReadFile("assets/" + name)
		return string(b), err
	}
	parts := map[string]string{}
	for key, name := range map[string]string{"__LEAFLET_CSS__": "leaflet.css", "__LEAFLET_JS__": "leaflet.js", "__MAP_CONTEXT__": "map-context.js", "page": "consensus.html"} {
		if parts[key], err = read(name); err != nil {
			return nil, err
		}
	}
	page := strings.NewReplacer("__LEAFLET_CSS__", parts["__LEAFLET_CSS__"], "__LEAFLET_JS__", parts["__LEAFLET_JS__"], "__MAP_CONTEXT__", parts["__MAP_CONTEXT__"], "__STATE_BORDERS__", string(borders), "__MAP_DATA__", string(payload)).Replace(parts["page"])
	return []byte(page), nil
}

// Refresh presents older saved results with current controls without rerunning
// the search or modifying its scores, evidence, or on-disk files.
func Refresh(page []byte) ([]byte, error) {
	if bytes.Contains(page, []byte(`<meta name="pinpoint-map-version" content="5">`)) {
		return page, nil
	}
	const marker = `<script id="map-data" type="application/json">`
	_, tail, ok := bytes.Cut(page, []byte(marker))
	if !ok {
		return nil, fmt.Errorf("saved map has no embedded search data")
	}
	data, _, ok := bytes.Cut(tail, []byte("</script>"))
	if !ok || !json.Valid(data) {
		return nil, fmt.Errorf("saved map contains invalid search data")
	}
	return Render(json.RawMessage(data))
}
