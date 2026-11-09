package search

import (
	"math"

	"pinpoint/internal/geo"
	"pinpoint/internal/tracks"
)

type trackEnvelope struct{ low, high geo.Vec }

type trackCell struct{ x, y, z int }
type trackIndex struct {
	tracks    []tracks.Track
	envelopes []trackEnvelope
	cells     map[trackCell][]int
	large     []int
	radius    float64
}

const trackCellSize = 20000.0

func cellFor(p geo.Vec) trackCell {
	return trackCell{int(math.Floor(p.X / trackCellSize)), int(math.Floor(p.Y / trackCellSize)), int(math.Floor(p.Z / trackCellSize))}
}

// Index expanded ECEF boxes. Every point that could hear a track is in one of
// its indexed cells. Large boxes use a fallback list to bound index memory.
func newTrackIndex(ts []tracks.Track, radius float64) *trackIndex {
	idx := &trackIndex{tracks: ts, envelopes: trackEnvelopes(ts), cells: map[trackCell][]int{}, radius: radius}
	pad := geo.Vec{X: radius + 0.001, Y: radius + 0.001, Z: radius + 0.001}
	for i, e := range idx.envelopes {
		if len(ts[i].Points) == 0 {
			continue
		}
		lo, hi := cellFor(e.low.Sub(pad)), cellFor(e.high.Add(pad))
		if float64(hi.x-lo.x+1)*float64(hi.y-lo.y+1)*float64(hi.z-lo.z+1) > 4096 {
			idx.large = append(idx.large, i)
			continue
		}
		for x := lo.x; x <= hi.x; x++ {
			for y := lo.y; y <= hi.y; y++ {
				for z := lo.z; z <= hi.z; z++ {
					key := trackCell{x, y, z}
					idx.cells[key] = append(idx.cells[key], i)
				}
			}
		}
	}
	return idx
}

func (idx *trackIndex) nearby(p geo.Vec) []tracks.Track {
	var out []tracks.Track
	local, large := idx.cells[cellFor(p)], idx.large
	for a, b := 0, 0; a < len(local) || b < len(large); {
		i := 0
		if b == len(large) || (a < len(local) && local[a] < large[b]) {
			i = local[a]
			a++
		} else {
			i = large[b]
			b++
		}
		e := idx.envelopes[i]
		dx := max(e.low.X-p.X, 0, p.X-e.high.X)
		dy := max(e.low.Y-p.Y, 0, p.Y-e.high.Y)
		dz := max(e.low.Z-p.Z, 0, p.Z-e.high.Z)
		if dx*dx+dy*dy+dz*dz <= idx.radius*idx.radius+1e-6 {
			out = append(out, idx.tracks[i])
		}
	}

	return out
}

func trackEnvelopes(ts []tracks.Track) []trackEnvelope {
	out := make([]trackEnvelope, len(ts))
	for i, t := range ts {
		e := trackEnvelope{geo.Vec{X: math.Inf(1), Y: math.Inf(1), Z: math.Inf(1)}, geo.Vec{X: math.Inf(-1), Y: math.Inf(-1), Z: math.Inf(-1)}}
		for _, p := range t.Points {
			e.low.X, e.high.X = min(e.low.X, p.Position.X), max(e.high.X, p.Position.X)
			e.low.Y, e.high.Y = min(e.low.Y, p.Position.Y), max(e.high.Y, p.Position.Y)
			e.low.Z, e.high.Z = min(e.low.Z, p.Position.Z), max(e.high.Z, p.Position.Z)
		}
		out[i] = e
	}
	return out
}

func nearbyTracks(p geo.Vec, ts []tracks.Track, envelopes []trackEnvelope, radius float64) []tracks.Track {
	var out []tracks.Track
	for i, e := range envelopes {
		dx := max(e.low.X-p.X, 0, p.X-e.high.X)
		dy := max(e.low.Y-p.Y, 0, p.Y-e.high.Y)
		dz := max(e.low.Z-p.Z, 0, p.Z-e.high.Z)
		if dx*dx+dy*dy+dz*dz <= radius*radius+1e-6 {
			out = append(out, ts[i])
		}
	}
	return out
}
