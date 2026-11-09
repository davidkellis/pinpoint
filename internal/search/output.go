package search

import (
	"bufio"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"pinpoint/internal/acoustic"
	"pinpoint/internal/geo"
	"pinpoint/internal/mapview"
	"pinpoint/internal/tracks"
)

func JSON(path string, v any) error {
	b, e := json.MarshalIndent(v, "", "  ")
	if e != nil {
		return e
	}
	return os.WriteFile(path, append(b, '\n'), 0644)
}
func WriteReport(dir string, r Report, c Config, s *acoustic.Spectrum, ts []tracks.Track) error {
	if err := loadEventSpectra(&c); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	if err := JSON(filepath.Join(dir, "results.json"), r); err != nil {
		return err
	}
	features := []any{}
	for i, p := range r.Candidates {
		features = append(features, map[string]any{"type": "Feature", "geometry": map[string]any{"type": "Point", "coordinates": []float64{p.Lon, p.Lat, p.Height}}, "properties": map[string]any{"rank": i + 1, "score_db": p.Score, "clock_offset_s": p.ClockOffset, "events": p.Events}})
	}
	if err := JSON(filepath.Join(dir, "candidates.geojson"), map[string]any{"type": "FeatureCollection", "features": features}); err != nil {
		return err
	}
	// Always truncate overlays, including on no-match runs, to avoid stale evidence.
	file, err := os.Create(filepath.Join(dir, "best-curves.csv"))
	if err != nil {
		return err
	}
	w := csv.NewWriter(file)
	var curves []acoustic.Curve
	err = w.Write([]string{"event", "track_id", "audio_time_s", "fundamental_hz", "score_at_curve_db"})
	if len(r.Candidates) > 0 && err == nil {
		best := r.Candidates[0]
		observer := geo.ECEF(best.Lat, best.Lon, best.Height)
		start, _ := time.Parse(time.RFC3339Nano, c.RecordingStart)
		epoch := float64(start.Unix()) + float64(start.Nanosecond())/1e9 + best.ClockOffset
		byID := map[string]tracks.Track{}
		for _, tr := range ts {
			byID[tr.ID] = tr
		}
		for i, e := range c.Events {
			source := eventSpectrum(c, s, i)
			match := best.Events[i]
			tr := byID[match.Track]
			for _, h := range e.Harmonics {
				curve := acoustic.Curve{Label: fmt.Sprintf("event %d, %s, harmonic %d", i+1, tr.ID, h)}
				for frame := range source.Rows {
					t := source.Time(frame)
					if t < e.Start || t > e.End {
						continue
					}
					factor, _, ok := tr.Received(observer, epoch+t, c.SoundSpeed, c.MaxGap)
					if ok {
						curve.Points = append(curve.Points, [2]float64{t, match.F0 * factor * float64(h)})
					}
				}
				curves = append(curves, curve)
			}
			for frame := range source.Rows {
				t := source.Time(frame)
				if t < e.Start || t > e.End {
					continue
				}
				factor, _, ok := tr.Received(observer, epoch+t, c.SoundSpeed, c.MaxGap)
				if !ok {
					continue
				}
				f := match.F0 * factor
				v, _ := source.Value(frame, f)
				if err = w.Write([]string{fmt.Sprint(i + 1), tr.ID, fmt.Sprintf("%.6f", t), fmt.Sprintf("%.6f", f), fmt.Sprintf("%.3f", v)}); err != nil {
					break
				}
			}
		}
	}
	w.Flush()
	if err == nil {
		err = w.Error()
	}
	ce := file.Close()
	if err != nil {
		return err
	}
	if ce != nil {
		return ce
	}
	if err := s.SVG(filepath.Join(dir, "best-overlay.svg"), curves); err != nil {
		return err
	}
	if err := writeScoreGrid(filepath.Join(dir, "score-grid.geojson"), r.ScoreGrid); err != nil {
		return err
	}
	evidence, err := BuildEvidence(r, c, s, ts)
	if err != nil {
		return err
	}
	if err = JSON(filepath.Join(dir, "evidence.json"), evidence); err != nil {
		return err
	}
	data := mapPayload(r, c, s.Source, ts)
	data.Evidence = &evidence
	return mapview.Write(filepath.Join(dir, "map.html"), data)
}

// Stream the full grid rather than building hundreds of thousands of feature
// maps alongside the already-retained search results.
func writeScoreGrid(path string, points []ScorePoint) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	if _, err = w.WriteString(`{"type":"FeatureCollection","features":[`); err != nil {
		return err
	}
	enc := json.NewEncoder(w)
	for i, p := range points {
		if i > 0 {
			if _, err = w.WriteString(","); err != nil {
				return err
			}
		}
		feature := struct {
			Type     string `json:"type"`
			Geometry struct {
				Type        string     `json:"type"`
				Coordinates [2]float64 `json:"coordinates"`
			} `json:"geometry"`
			Properties ScorePoint `json:"properties"`
		}{Type: "Feature", Properties: p}
		feature.Geometry.Type = "Point"
		feature.Geometry.Coordinates = [2]float64{p.Lon, p.Lat}
		if err = enc.Encode(feature); err != nil {
			return err
		}
	}
	if _, err = w.WriteString("]}\n"); err != nil {
		return err
	}
	if err = w.Flush(); err != nil {
		return err
	}
	return f.Close()
}

type mapFlight struct {
	Event    int            `json:"event"`
	TrackID  string         `json:"track_id"`
	Segments [][][2]float64 `json:"segments"`
}
type mapReport struct {
	Report     Report      `json:"report"`
	Source     string      `json:"source"`
	EventCount int         `json:"event_count"`
	Flights    []mapFlight `json:"flight_paths"`
	Region     string      `json:"region_mask,omitempty"`
	Evidence   *Evidence   `json:"evidence,omitempty"`
}

func mapPayload(r Report, c Config, source string, ts []tracks.Track) mapReport {
	data := mapReport{Report: r, Source: source, EventCount: len(c.Events), Flights: []mapFlight{}, Region: c.RegionMask}
	if len(r.Candidates) == 0 {
		return data
	}
	best := r.Candidates[0]
	base, _ := time.Parse(time.RFC3339Nano, c.RecordingStart)
	start := float64(base.Unix()) + float64(base.Nanosecond())/1e9 + best.ClockOffset
	byID := map[string]tracks.Track{}
	for _, t := range ts {
		byID[t.ID] = t
	}
	for i, match := range best.Events {
		e := c.Events[i]
		flight := mapFlight{Event: i + 1, TrackID: match.Track, Segments: [][][2]float64{}}
		var segment [][2]float64
		last := 0.0
		flush := func() {
			if len(segment) > 1 {
				flight.Segments = append(flight.Segments, segment)
			}
			segment = nil
		}
		for _, p := range byID[match.Track].Points {
			if p.Time < start+e.Start-e.MaxSlant/c.SoundSpeed-c.MaxGap || p.Time > start+e.End+c.MaxGap {
				continue
			}
			if p.Time-last > c.MaxGap {
				flush()
			}
			segment = append(segment, [2]float64{p.Lat, p.Lon})
			last = p.Time
		}
		flush()
		if len(flight.Segments) > 0 {
			data.Flights = append(data.Flights, flight)
		}
	}
	return data
}
