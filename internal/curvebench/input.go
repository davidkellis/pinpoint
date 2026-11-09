package curvebench

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"math"
	"math/rand"
	"os"
	"os/exec"
	"strconv"

	"pinpoint/internal/acoustic"
)

type Injection struct {
	Kind  string  `json:"kind"`
	SNRDB float64 `json:"snr_db"`
}
type Case struct {
	ID       string                 `json:"id"`
	Label    string                 `json:"label"`
	Kind     string                 `json:"kind"` // reference, unlabeled, injection, non_doppler, other_doppler
	Audio    string                 `json:"audio,omitempty"`
	Start    float64                `json:"start_s"`
	Duration float64                `json:"duration_s"`
	Seed     int64                  `json:"seed"`
	Control  string                 `json:"control,omitempty"`
	Inject   *Injection             `json:"inject,omitempty"`
	Anchors  []acoustic.RidgeSample `json:"anchors,omitempty"` // seconds relative to this clip; evaluator only
}

func decode(ctx context.Context, c Case) ([]float64, string, error) {
	var audio []float64
	hash := "synthetic"
	if c.Audio != "" {
		file, err := os.Open(c.Audio)
		if err != nil {
			return nil, "", err
		}
		h := sha256.New()
		_, err = io.Copy(h, file)
		file.Close()
		if err != nil {
			return nil, "", err
		}
		hash = hex.EncodeToString(h.Sum(nil))
		cmd := exec.CommandContext(ctx, "ffmpeg", "-nostdin", "-v", "error", "-ss", strconv.FormatFloat(c.Start, 'f', 6, 64), "-i", c.Audio, "-t", strconv.FormatFloat(c.Duration, 'f', 6, 64), "-map", "0:a:0", "-vn", "-af", "pan=mono|c0=c0", "-ar", "4096", "-c:a", "pcm_f32le", "-f", "f32le", "pipe:1")
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		b, err := cmd.Output()
		if err != nil {
			return nil, "", fmt.Errorf("ffmpeg: %w: %s", err, stderr.String())
		}
		audio = make([]float64, len(b)/4)
		for i := range audio {
			audio[i] = float64(math.Float32frombits(binary.LittleEndian.Uint32(b[4*i:])))
		}
	} else {
		audio = make([]float64, int(c.Duration*Rate))
		rng := rand.New(rand.NewSource(c.Seed))
		for i := range audio {
			t := float64(i) / Rate
			audio[i] = .025*rng.NormFloat64() + .04*math.Sin(2*math.Pi*60*t) + .015*math.Sin(2*math.Pi*183*t)
			if c.Control == "chirp" {
				audio[i] += .03 * math.Sin(2*math.Pi*(350*t-.8*t*t))
			}
			if c.Control == "bursts" {
				pulse := math.Exp(-math.Pow(math.Mod(t, .65)/.04, 2))
				audio[i] += .15 * pulse * rng.NormFloat64()
			}
		}
	}
	if float64(len(audio))/Rate < c.Duration-.25 {
		return nil, "", fmt.Errorf("requested %.2f s but only decoded %.2f s; shorten the manifest duration", c.Duration, float64(len(audio))/Rate)
	}
	if c.Control == "shuffle" { // 8 s blocks: an intentionally disrupted reference, not a labeled quiet walk
		block := 8 * Rate
		chunks := len(audio) / block
		rng := rand.New(rand.NewSource(c.Seed))
		permutation := rng.Perm(chunks)
		copyOf := append([]float64(nil), audio...)
		for i, j := range permutation {
			copy(audio[i*block:(i+1)*block], copyOf[j*block:(j+1)*block])
		}
	}
	return audio, hash, nil
}
func inject(audio []float64, kind string, snrDB float64) ([]float64, []acoustic.RidgeSample) {
	v, d, f0 := 100., 2500., 130.
	harmonics := []float64{1, .5, .3}
	if kind == "jet" {
		v, d, f0 = 180, 4000, 800
		harmonics = []float64{1}
	}
	if kind == "vehicle" {
		v, d, f0 = 25, 100, 140
	}
	duration := float64(len(audio)) / Rate
	cpa := duration * .5
	tone := make([]float64, len(audio))
	var anchors []acoustic.RidgeSample
	for i := range tone {
		t := float64(i) / Rate
		arrival := t - cpa + d/343
		lo, hi := arrival-math.Max(300, (d+v*math.Abs(arrival))/(343-v)+1), arrival
		for j := 0; j < 35; j++ {
			te := (lo + hi) / 2
			if te+math.Hypot(d, v*te)/343 > arrival {
				hi = te
			} else {
				lo = te
			}
		}
		te := (lo + hi) / 2
		envelope := math.Min(1, math.Min(t/8, (duration-t)/8)) * d / math.Hypot(d, v*te)
		for h, a := range harmonics {
			tone[i] += a * envelope * math.Sin(2*math.Pi*f0*float64(h+1)*te)
		}
		if i%Rate == 0 && t >= 20 && t <= duration-20 {
			anchors = append(anchors, acoustic.RidgeSample{Time: t, Hz: f0 * 343 / (343 + v*v*te/math.Hypot(d, v*te))})
		}
	}
	noisePower, tonePower := 0., 0.
	for i := range tone {
		noisePower += audio[i] * audio[i]
		tonePower += tone[i] * tone[i]
	}
	scale := math.Sqrt(noisePower / math.Max(tonePower, 1e-30) * math.Pow(10, snrDB/10))
	mixed := append([]float64(nil), audio...)
	for i := range mixed {
		mixed[i] += tone[i] * scale
	}
	return mixed, anchors
}
