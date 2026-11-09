// Package acoustic extracts acoustic evidence, not ADS-B radio packets.
package acoustic

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/gob"
	"fmt"
	"image"
	"image/color"
	"io"
	"math"
	"math/cmplx"
	"os"
	"os/exec"
	"sort"
	"strconv"
)

type Options struct {
	Rate, Window, Hop int
	MinHz, MaxHz      float64
	Channel           int
}

func Defaults() Options { return Options{4096, 8192, 1024, 40, 1800, 0} }

type Spectrum struct {
	discoveryProfile                  string // per-call policy; never persisted in spectral caches
	Version                           int
	Source                            string
	SampleRate, Window, Hop, FirstBin int
	// TimeOffset preserves recording-relative times in overlapping analysis blocks.
	TimeOffset float64
	// Row-major whitened log power (dB above temporal and spectral background).
	Rows [][]float32
}

func (s *Spectrum) Time(i int) float64 {
	return s.TimeOffset + (float64(i*s.Hop)+float64(s.Window)/2)/float64(s.SampleRate)
}
func (s *Spectrum) Hz(i int) float64 {
	return float64(s.FirstBin+i) * float64(s.SampleRate) / float64(s.Window)
}
func (s *Spectrum) Step() float64 { return float64(s.Hop) / float64(s.SampleRate) }
func (s *Spectrum) Value(frame int, hz float64) (float64, bool) {
	if frame < 0 || frame >= len(s.Rows) {
		return 0, false
	}
	x := hz*float64(s.Window)/float64(s.SampleRate) - float64(s.FirstBin)
	if !finite(x) || x < 0 || x > float64(len(s.Rows[frame])-1) {
		return 0, false
	}
	i := int(x)
	j := min(i+1, len(s.Rows[frame])-1)
	u := x - float64(i)
	return float64(s.Rows[frame][i])*(1-u) + float64(s.Rows[frame][j])*u, true
}
func finite(x float64) bool { return !math.IsNaN(x) && !math.IsInf(x, 0) }

// FFT is an in-place radix-2 transform; length is validated by the caller.
func FFT(a []complex128) {
	n := len(a)
	for i, j := 1, 0; i < n; i++ {
		bit := n >> 1
		for ; j&bit != 0; bit >>= 1 {
			j ^= bit
		}
		j ^= bit
		if i < j {
			a[i], a[j] = a[j], a[i]
		}
	}
	for size := 2; size <= n; size *= 2 {
		root := cmplx.Rect(1, -2*math.Pi/float64(size))
		for start := 0; start < n; start += size {
			w := complex(1.0, 0)
			for j := 0; j < size/2; j++ {
				u, v := a[start+j], w*a[start+j+size/2]
				a[start+j], a[start+j+size/2] = u+v, u-v
				w *= root
			}
		}
	}
}

func Analyze(ctx context.Context, path string, o Options) (*Spectrum, error) {
	return analyzeSpan(ctx, path, o, 0, 0)
}

func analyzeSpan(ctx context.Context, path string, o Options, from, duration float64) (*Spectrum, error) {
	return decodeSpan(ctx, path, o, from, duration, true)
}

func decodeSpan(ctx context.Context, path string, o Options, from, duration float64, whiten bool) (*Spectrum, error) {
	if o.Rate < 1000 || o.Rate > 48000 || o.Window < 256 || o.Window > 65536 || o.Window&(o.Window-1) != 0 || o.Hop < 1 || o.Hop > o.Window || o.MinHz < 0 || o.MaxHz <= o.MinHz || o.MaxHz >= float64(o.Rate)/2 || o.Channel < 0 {
		return nil, fmt.Errorf("invalid STFT options")
	}
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}
	// Keep a single channel: averaging stereo can cancel low-frequency signals.
	args := []string{"-nostdin", "-v", "error"}
	if from > 0 {
		args = append(args, "-ss", strconv.FormatFloat(from, 'f', 9, 64))
	}
	args = append(args, "-i", path)
	if duration > 0 {
		args = append(args, "-t", strconv.FormatFloat(duration, 'f', 9, 64))
	}
	args = append(args, "-map", "0:a:0", "-vn", "-af", fmt.Sprintf("pan=mono|c0=c%d", o.Channel), "-ar", strconv.Itoa(o.Rate), "-c:a", "pcm_f32le", "-f", "f32le", "pipe:1")
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err = cmd.Start(); err != nil {
		return nil, fmt.Errorf("start ffmpeg (required for audio decoding): %w", err)
	}
	s, readErr := fromPCM(stdout, path, o, whiten)
	if readErr != nil {
		_ = cmd.Process.Kill()
	}
	waitErr := cmd.Wait()
	if readErr != nil {
		return nil, readErr
	}
	if waitErr != nil {
		return nil, fmt.Errorf("ffmpeg: %w: %s", waitErr, stderr.String())
	}
	s.TimeOffset = from
	return s, nil
}

func FromPCM(reader io.Reader, source string, o Options) (*Spectrum, error) {
	return fromPCM(reader, source, o, true)
}

func fromPCM(reader io.Reader, source string, o Options, whiten bool) (*Spectrum, error) {
	s := &Spectrum{Version: 1, Source: source, SampleRate: o.Rate, Window: o.Window, Hop: o.Hop, FirstBin: int(math.Ceil(o.MinHz * float64(o.Window) / float64(o.Rate)))}
	last := int(math.Floor(o.MaxHz * float64(o.Window) / float64(o.Rate)))
	r := bufio.NewReader(reader)
	samples := make([]float64, o.Window)
	buf := make([]byte, 4*o.Window)
	read := func(dst []float64) error {
		_, err := io.ReadFull(r, buf[:len(dst)*4])
		if err != nil {
			return err
		}
		for i := range dst {
			dst[i] = float64(math.Float32frombits(binary.LittleEndian.Uint32(buf[4*i : 4*i+4])))
			if !finite(dst[i]) {
				return fmt.Errorf("nonfinite audio sample")
			}
		}
		return nil
	}
	if err := read(samples); err != nil {
		return nil, fmt.Errorf("audio shorter than one analysis window: %w", err)
	}
	window := make([]float64, o.Window)
	for i := range window {
		window[i] = 0.5 - 0.5*math.Cos(2*math.Pi*float64(i)/float64(o.Window-1))
	}
	a := make([]complex128, o.Window)
	for {
		if (len(s.Rows)+1)*(last-s.FirstBin+1) > 100_000_000 {
			return nil, fmt.Errorf("spectrogram exceeds 100 million cells; split the audio into shorter clips")
		}
		for i := range a {
			a[i] = complex(samples[i]*window[i], 0)
		}
		FFT(a)
		row := make([]float32, last-s.FirstBin+1)
		for i := range row {
			v := a[i+s.FirstBin]
			row[i] = float32(10 * math.Log10(real(v)*real(v)+imag(v)*imag(v)+1e-20))
		}
		s.Rows = append(s.Rows, row)
		copy(samples, samples[o.Hop:])
		err := read(samples[o.Window-o.Hop:])
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			break
		}
		if err != nil {
			return nil, err
		}
	}
	if len(s.Rows) < 2 {
		return nil, fmt.Errorf("audio must contain at least two analysis frames")
	}
	if whiten {
		s.whiten()
	}
	return s, nil
}
func median(a []float64) float64 {
	// Whitening needs one order statistic, not a full sort of every frequency
	// history. Three-way quickselect handles constant hum and repeated values;
	// a work budget falls back to sorting on adversarial arrangements.
	k, lo, hi := len(a)/2, 0, len(a)
	budget := 2*int(math.Log2(float64(max(1, len(a))))) + 1
	for hi-lo > 32 && budget > 0 {
		budget--
		x, y, z := a[lo], a[(lo+hi)/2], a[hi-1]
		if x > y {
			x, y = y, x
		}
		if y > z {
			y, z = z, y
		}
		if x > y {
			x, y = y, x
		}
		pivot := y
		left, scan, right := lo, lo, hi
		for scan < right {
			if a[scan] < pivot {
				a[left], a[scan] = a[scan], a[left]
				left++
				scan++
			} else if a[scan] > pivot {
				right--
				a[right], a[scan] = a[scan], a[right]
			} else {
				scan++
			}
		}
		if k < left {
			hi = left
		} else if k >= right {
			lo = right
		} else {
			return pivot
		}
	}
	sort.Float64s(a[lo:hi])
	return a[k]
}
func (s *Spectrum) whiten() {
	// Per-frequency median suppresses stationary hum; per-frame median reduces
	// gain changes and broadband events. Scores remain heuristic, not likelihoods.
	col := make([]float64, len(s.Rows))
	for j := range s.Rows[0] {
		for i := range s.Rows {
			col[i] = float64(s.Rows[i][j])
		}
		m := median(col)
		for i := range s.Rows {
			s.Rows[i][j] -= float32(m)
		}
	}
	row := make([]float64, len(s.Rows[0]))
	for i := range s.Rows {
		for j, v := range s.Rows[i] {
			row[j] = float64(v)
		}
		m := median(row)
		for j := range s.Rows[i] {
			s.Rows[i][j] -= float32(m)
		}
	}
}
func (s *Spectrum) Save(path string) error {
	f, e := os.Create(path)
	if e != nil {
		return e
	}
	err := gob.NewEncoder(f).Encode(s)
	closeErr := f.Close()
	if err != nil {
		return err
	}
	return closeErr
}
func Load(path string) (*Spectrum, error) {
	f, e := os.Open(path)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	var s Spectrum
	if e = gob.NewDecoder(io.LimitReader(f, 512<<20)).Decode(&s); e != nil {
		return nil, e
	}
	if s.Version != 1 || s.SampleRate <= 0 || s.Window <= 0 || s.Hop <= 0 || !finite(s.TimeOffset) || s.TimeOffset < 0 || s.FirstBin < 0 || len(s.Rows) < 2 || len(s.Rows[0]) < 2 {
		return nil, fmt.Errorf("invalid or unsupported spectrogram")
	}
	for _, r := range s.Rows {
		if len(r) != len(s.Rows[0]) {
			return nil, fmt.Errorf("ragged spectrogram")
		}
		for _, v := range r {
			if !finite(float64(v)) {
				return nil, fmt.Errorf("nonfinite spectrogram")
			}
		}
	}
	return &s, nil
}
func (s *Spectrum) plotImage() image.Image {
	w, h := min(len(s.Rows), 2400), len(s.Rows[0])
	im := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := 0; x < w; x++ {
		lo, hi := x*len(s.Rows)/w, (x+1)*len(s.Rows)/w
		for j := 0; j < h; j++ {
			v := 0.0
			for i := lo; i < hi; i++ {
				v = max(v, float64(s.Rows[i][j]))
			}
			v = min(v/30, 1)
			im.Set(x, h-1-j, color.RGBA{uint8(255 * math.Sqrt(v)), uint8(230 * v * v), uint8(80 * v), 255})
		}
	}
	return im
}
func (s *Spectrum) PNG(path string) error {
	f, e := os.Create(path)
	if e != nil {
		return e
	}
	err := encodePreview(f, s.plotImage())
	ce := f.Close()
	if err != nil {
		return err
	}
	return ce
}
