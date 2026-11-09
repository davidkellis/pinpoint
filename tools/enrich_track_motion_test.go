//go:build ignore

package main

import (
	"testing"

	"pinpoint/internal/geo"
	"pinpoint/internal/tracks"
)

func TestMotionEnrichmentRequiresSameAircraftTimeAndPosition(t *testing.T) {
	p := tracks.Point{Time: 1005, Lat: 29, Lon: -98, Alt: 500, Position: geo.ECEF(29, -98, 500)}
	old := tracks.Track{ID: "abc123-1000.000-7", TypeCode: "C172", Points: []tracks.Point{p}}
	m := tracks.KinematicPoint{Time: p.Time, GroundSpeed: 40, TrueTrack: 90, Horizontal: true}
	source := tracks.Track{ID: "abc123-0.000-99", TypeCode: "C172", Points: []tracks.Point{p}, Motion: []tracks.KinematicPoint{m}}
	out, counts := attachMotion(old, []tracks.Track{source})
	if counts.Horizontal != 1 || len(out.Motion) != 1 || out.Points[0] != p || len(old.Motion) != 0 {
		t.Fatal("lost motion, moved original geometry or mutated input", out, counts)
	}
	for _, kind := range []string{"aircraft", "time", "position", "type"} {
		s := source
		s.Points = append([]tracks.Point(nil), source.Points...)
		switch kind {
		case "aircraft":
			s.ID = "def456-0.000-1"
		case "time":
			s.Points[0].Time += .02
		case "position":
			s.Points[0].Position = s.Points[0].Position.Add(geo.Vec{X: 2})
		case "type":
			s.TypeCode = "B738"
		}
		r, _ := attachMotion(old, []tracks.Track{s})
		if len(r.Motion) > 0 {
			t.Fatal("attached mismatched motion", kind)
		}
	}
}
