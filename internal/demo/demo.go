// Package demo creates reproducible acoustic fixtures using continuous flight
// paths and emitted phase, independently of the trajectory interpolation/scorer.
package demo

import (
	"encoding/binary"
	"encoding/json"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"time"

	"pinpoint/internal/acoustic"
	"pinpoint/internal/geo"
	"pinpoint/internal/search"
	"pinpoint/internal/tracks"
)

const Lat = 35.25
const Lon = -100.5
const Height = 700.0
const Epoch = 1700000000.0
const Duration = 220

func path(flight int, t float64) geo.Vec {
	origin := geo.ECEF(Lat, Lon, Height)
	la, lo := Lat*math.Pi/180, Lon*math.Pi/180
	east := geo.Vec{X: -math.Sin(lo), Y: math.Cos(lo)}
	north := geo.Vec{X: -math.Sin(la) * math.Cos(lo), Y: -math.Sin(la) * math.Sin(lo), Z: math.Cos(la)}
	up := geo.Vec{X: math.Cos(la) * math.Cos(lo), Y: math.Cos(la) * math.Sin(lo), Z: math.Sin(la)}
	u := t - 65
	x, y, z := 80*u+0.04*u*u, 500+0.02*u*u, 1100+0.5*u
	if flight%2 == 1 {
		u = t - 165
		x, y, z = -650+0.035*u*u, 95*u-0.025*u*u, 1400-0.4*u
	}
	if flight >= 2 {
		x += 4500
		y -= 3000
	}
	return origin.Add(east.Scale(x)).Add(north.Scale(y)).Add(up.Scale(z))
}
func Generate(dir string) (search.Config, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return search.Config{}, err
	}
	var ts []tracks.Track
	for f := 0; f < 4; f++ {
		tr := tracks.Track{ID: []string{"synthetic-A", "synthetic-B", "distractor-C", "distractor-D"}[f]}
		for t := -60; t <= Duration+60; t += 2 {
			p := path(f, float64(t))
			lat, lon, h := geo.Geodetic(p)
			tr.Points = append(tr.Points, tracks.Point{Time: Epoch + float64(t), Lat: lat, Lon: lon, Alt: h, Position: p})
		}
		ts = append(ts, tr)
	}
	file, err := os.Create(filepath.Join(dir, "tracks.csv"))
	if err != nil {
		return search.Config{}, err
	}
	err = tracks.Write(file, ts)
	ce := file.Close()
	if err == nil {
		err = ce
	}
	if err != nil {
		return search.Config{}, err
	}
	rate := 4096
	pcm := make([]int16, Duration*rate)
	rng := rand.New(rand.NewSource(73))
	observer := geo.ECEF(Lat, Lon, Height)
	for i := range pcm {
		tr := float64(i) / float64(rate)
		v := 0.02*rng.NormFloat64() + 0.03*math.Sin(2*math.Pi*60*tr)
		for flight := 0; flight < 2; flight++ {
			// Bisection of the continuous path supplies phase at emission time. This
			// does not use tracks.Received or the frequency-scoring implementation.
			lo, hi := tr-150, tr
			for k := 0; k < 32; k++ {
				mid := (lo + hi) / 2
				if mid+path(flight, mid).Sub(observer).Norm()/343 > tr {
					hi = mid
				} else {
					lo = mid
				}
			}
			te := (lo + hi) / 2
			d := path(flight, te).Sub(observer).Norm()
			center := 65.0
			f0 := 130.0
			if flight == 1 {
				center = 165
				f0 = 160
			}
			envelope := math.Exp(-math.Pow((te-center)/48, 8)) * 1200 / d
			v += envelope * (0.09*math.Sin(2*math.Pi*f0*te) + 0.035*math.Sin(4*math.Pi*f0*te))
		}
		pcm[i] = int16(max(-1.0, min(1.0, v)) * 32767)
	}
	file, err = os.Create(filepath.Join(dir, "audio.wav"))
	if err != nil {
		return search.Config{}, err
	}
	err = writeWAV(file, pcm, rate)
	ce = file.Close()
	if err == nil {
		err = ce
	}
	if err != nil {
		return search.Config{}, err
	}
	c := search.DefaultConfig()
	c.Spectrum = "spectrogram.bin"
	c.Tracks = "tracks.csv"
	c.RecordingStart = time.Unix(int64(Epoch)-2, 0).UTC().Format(time.RFC3339)
	c.ClockUncertainty = 4
	c.ClockStep = 0.5
	c.Grid = 250
	c.Refine = 50
	h := Height
	c.ObserverHeight = &h
	c.Bounds = geo.Bounds{South: Lat - 0.012, North: Lat + 0.012, West: Lon - 0.015, East: Lon + 0.015}
	c.Events = []acoustic.Event{{Start: 30, End: 103, CPATime: 69, FMin: 127, FMax: 133, FStep: 0.5, Harmonics: []int{1, 2}, MinSpeed: 60, MaxSpeed: 110, MinSlant: 500, MaxSlant: 4000}, {Start: 130, End: 203, CPATime: 170, FMin: 157, FMax: 163, FStep: 0.5, Harmonics: []int{1, 2}, MinSpeed: 60, MaxSpeed: 120, MinSlant: 500, MaxSlant: 4000}}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return c, err
	}
	err = os.WriteFile(filepath.Join(dir, "search.json"), append(b, '\n'), 0644)
	if err != nil {
		return c, err
	}
	truth := map[string]any{"lat": Lat, "lon": Lon, "height_ellipsoid_m": Height, "clock_offset_s": 2, "synthetic": true}
	b, err = json.MarshalIndent(truth, "", "  ")
	if err != nil {
		return c, err
	}
	return c, os.WriteFile(filepath.Join(dir, "truth.json"), append(b, '\n'), 0644)
}
func writeWAV(f *os.File, samples []int16, rate int) error {
	size := len(samples) * 2
	header := make([]byte, 44)
	copy(header, "RIFF")
	binary.LittleEndian.PutUint32(header[4:], uint32(36+size))
	copy(header[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(header[16:], 16)
	binary.LittleEndian.PutUint16(header[20:], 1)
	binary.LittleEndian.PutUint16(header[22:], 1)
	binary.LittleEndian.PutUint32(header[24:], uint32(rate))
	binary.LittleEndian.PutUint32(header[28:], uint32(rate*2))
	binary.LittleEndian.PutUint16(header[32:], 2)
	binary.LittleEndian.PutUint16(header[34:], 16)
	copy(header[36:], "data")
	binary.LittleEndian.PutUint32(header[40:], uint32(size))
	if _, e := f.Write(header); e != nil {
		return e
	}
	return binary.Write(f, binary.LittleEndian, samples)
}
