package search

import (
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"

	"pinpoint/internal/acoustic"
	"pinpoint/internal/tracks"
)

// The cache binds every measured spectrum value, trajectory coordinate/time,
// search setting and executable byte. Interrupted/failed searches are not cached.
func automaticFingerprint(ctx context.Context, s *acoustic.Spectrum, ts []tracks.Track) (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	if _, err := os.Stat("/proc/self/exe"); err == nil {
		executable = "/proc/self/exe"
	}
	return automaticFingerprintForBinary(ctx, s, ts, executable)
}

func automaticFingerprintForBinary(ctx context.Context, s *acoustic.Spectrum, ts []tracks.Track, executable string) (string, error) {
	h := sha256.New()
	f, err := os.Open(executable)
	if err != nil {
		return "", err
	}
	_, err = io.Copy(h, f)
	f.Close()
	if err != nil {
		return "", err
	}
	enc := json.NewEncoder(h)
	if err = enc.Encode([]int{s.Version, s.SampleRate, s.Window, s.Hop, s.FirstBin, len(s.Rows)}); err != nil {
		return "", err
	}
	if err = enc.Encode(s.TimeOffset); err != nil {
		return "", err
	}
	var buf []byte
	for _, row := range s.Rows {
		if err = ctx.Err(); err != nil {
			return "", err
		}
		if len(buf) < 4*len(row) {
			buf = make([]byte, 4*len(row))
		}
		b := buf[:4*len(row)]
		for k, v := range row {
			binary.LittleEndian.PutUint32(b[4*k:], math.Float32bits(v))
		}
		h.Write(b)
	}
	var point [56]byte
	for _, t := range ts {
		if err = ctx.Err(); err != nil {
			return "", err
		}
		if err = enc.Encode(struct {
			ID, Type string
			N        int
		}{t.ID, t.TypeCode, len(t.Points)}); err != nil {
			return "", err
		}
		for _, p := range t.Points {
			for i, v := range []float64{p.Time, p.Lat, p.Lon, p.Alt, p.Position.X, p.Position.Y, p.Position.Z} {
				binary.LittleEndian.PutUint64(point[i*8:], math.Float64bits(v))
			}
			h.Write(point[:])
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

type ModelInput struct {
	Path    string `json:"path"`
	SHA256  string `json:"sha256,omitempty"`
	Missing bool   `json:"missing,omitempty"`
}

// Bind cached geography to the bytes of every potentially used DEM/geoid tile,
// including wrong-time trial footprints. Creating a previously absent tile or
// correcting an elevation invalidates earlier cached fits.
func automaticModelInputs(ctx context.Context, c Config, ts []tracks.Track, o AutomaticOptions) ([]ModelInput, error) {
	if c.ObserverHeight != nil {
		return nil, nil
	}
	var paths []string
	if c.GeoidGrid != "" {
		paths = append(paths, c.GeoidGrid)
	}
	extent := 0.
	for _, shift := range o.ControlShifts {
		extent = math.Max(extent, math.Abs(shift))
	}
	c.ClockUncertainty += extent
	c.Corridors = true
	c.Grid = math.Min(c.Grid, 200)
	names, err := TerrainTiles(ctx, c, ts)
	if err != nil {
		return nil, err
	}
	for _, name := range names {
		paths = append(paths, filepath.Join(c.TerrainDir, name))
	}
	sort.Strings(paths)
	var out []ModelInput
	for _, p := range paths {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		x := ModelInput{Path: p}
		f, err := os.Open(p)
		if os.IsNotExist(err) {
			x.Missing = true
			out = append(out, x)
			continue
		}
		if err != nil {
			return nil, err
		}
		h := sha256.New()
		_, err = io.Copy(h, f)
		f.Close()
		if err != nil {
			return nil, err
		}
		x.SHA256 = hex.EncodeToString(h.Sum(nil))
		out = append(out, x)
	}
	return out, nil
}

func bindModelInputs(key string, inputs []ModelInput) string {
	h := sha256.New()
	io.WriteString(h, key)
	json.NewEncoder(h).Encode(inputs)
	return hex.EncodeToString(h.Sum(nil))
}

func cachedAutomaticRun(ctx context.Context, dir, key string, c Config, run func() (Report, error)) (Report, error) {
	if dir == "" {
		return run()
	}
	b, err := json.Marshal(c)
	if err != nil {
		return Report{}, err
	}
	h := sha256.New()
	h.Write([]byte(key))
	h.Write(b)
	path := filepath.Join(dir, hex.EncodeToString(h.Sum(nil))+".json.gz")
	if f, err := os.Open(path); err == nil {
		z, err := gzip.NewReader(f)
		if err == nil {
			var r Report
			err = json.NewDecoder(z).Decode(&r)
			z.Close()
			f.Close()
			if err == nil {
				return r, ctx.Err()
			}
		} else {
			f.Close()
		}
	}
	r, err := run()
	if err != nil {
		return r, err
	}
	if err = os.MkdirAll(dir, 0700); err != nil {
		return r, err
	}
	f, err := os.CreateTemp(dir, ".group-*")
	if err != nil {
		return r, err
	}
	defer os.Remove(f.Name())
	z := gzip.NewWriter(f)
	err = json.NewEncoder(z).Encode(r)
	ze := z.Close()
	fe := f.Close()
	if err != nil {
		return r, err
	}
	if ze != nil {
		return r, ze
	}
	if fe != nil {
		return r, fe
	}
	return r, os.Rename(f.Name(), path)
}
