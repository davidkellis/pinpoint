//go:build ignore

package main

import (
	"math"
	"pinpoint/internal/acoustic"
	"pinpoint/internal/geo"
	"pinpoint/internal/tracks"
	"testing"
)

func TestRegionIndexKeepsEveryNearbySegmentAndHeight(t *testing.T) {
	tr := tracks.Track{ID: "crossing", Points: []tracks.Point{{Position: geo.Vec{X: -60000, Y: 12000, Z: 30000}}, {Position: geo.Vec{X: 60000, Y: 14000, Z: 36000}}}}
	index := regionTrackIndex([]tracks.Track{tr}, 15000)
	for u := 0.; u <= 1; u += .03 {
		p := tr.Points[0].Position.Add(tr.Points[1].Position.Sub(tr.Points[0].Position).Scale(u))
		for _, d := range []geo.Vec{{X: 14999}, {Y: -14999}, {Z: -14999}, {X: 8000, Y: 8000, Z: 8000}} {
			if len(index(p.Add(d))) != 1 {
				t.Fatal("spatial index removed eligible interior geometry", u, d)
			}
		}
	}
	if len(index(geo.Vec{X: math.Pow(10, 7), Y: math.Pow(10, 7), Z: math.Pow(10, 7)})) != 0 {
		t.Fatal("distant geometry not screened")
	}
}

func TestMapSamplingPreservesWinnersWithoutChangingScoredPoints(t *testing.T) {
	var all []regionPoint
	for i := 0; i < 1000; i++ {
		spacing := 1000.
		if i%10 == 0 {
			spacing = 20
		}
		all = append(all, regionPoint{Lat: 30, Lon: -100 + float64(i)*.001, Spacing: spacing, Rank: float64(i), Valid: true})
	}
	winner := all[999]
	sample := displayRegionPoints(all, []regionPoint{winner}, 100, 1000)
	if len(sample) > 100 || len(sample) == 0 || sample[0].Lon != winner.Lon || len(all) != 1000 || all[0].Rank != 0 || all[999].Rank != 999 {
		t.Fatal("display thinning changed results, lost the winner or exceeded the cap")
	}
}

func TestRegionScreenRetainsCrossingSegmentsAndInterWindowGaps(t *testing.T) {
	b := geo.Bounds{South: 29, North: 29.1, West: -98.1, East: -98}
	tr := tracks.Track{ID: "crossing"}
	for _, p := range []tracks.Point{{Time: 0, Lat: 29, Lon: -99}, {Time: 100, Lat: 29, Lon: -97}, {Time: 1000, Lat: 29, Lon: -99}, {Time: 1100, Lat: 29, Lon: -97}} {
		p.Position = geo.ECEF(p.Lat, p.Lon, 1000)
		tr.Points = append(tr.Points, p)
		tr.Motion = append(tr.Motion, tracks.KinematicPoint{Time: p.Time, Horizontal: true, GroundSpeed: 80, TrueTrack: 90})
	}
	e := []acoustic.Event{{Start: 45, End: 55}, {Start: 1045, End: 1055}}
	out := trimRegionTracks([]tracks.Track{tr}, b, e, 0, 6)
	if len(out) != 1 || len(out[0].Points) != 4 || len(out[0].Motion) != 4 {
		t.Fatal("discarded a crossing with endpoints outside region or bridged a long gap", out)
	}
	far := tr
	far.Points = append([]tracks.Point(nil), tr.Points...)
	for i := range far.Points {
		far.Points[i].Lat += 10
	}
	if len(trimRegionTracks([]tracks.Track{far}, b, e, 0, 6)) != 0 {
		t.Fatal("retained distant geometry")
	}
	if len(trimRegionTracks([]tracks.Track{tr}, b, e, 10000, 6)) != 0 {
		t.Fatal("retained wrong-date geometry")
	}
}
