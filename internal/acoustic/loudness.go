package acoustic

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"sort"
	"strconv"
)

// Loudness is a band-limited RMS level per Step seconds of audio, in dB
// relative to full scale. Unlike the whitened spectrogram, it keeps broadband
// level changes, such as the rise of a nearby aircraft pass.
type Loudness struct {
	Step  float64   `json:"step_s"`
	Level []float64 `json:"level_db"`
}

// LoudnessEnvelope decodes one channel with FFmpeg, keeps lowHz..highHz, and
// returns the RMS level per step seconds.
func LoudnessEnvelope(ctx context.Context, path string, channel int, lowHz, highHz, step float64) (Loudness, error) {
	const rate = 8000
	if channel < 0 || !(lowHz > 0) || !(highHz > lowHz) || highHz >= rate/2 || !(step >= .1) || step > 10 {
		return Loudness{}, fmt.Errorf("invalid loudness options")
	}
	if _, err := os.Stat(path); err != nil {
		return Loudness{}, err
	}
	filter := fmt.Sprintf("pan=mono|c0=c%d,highpass=f=%g,lowpass=f=%g", channel, lowHz, highHz)
	cmd := exec.CommandContext(ctx, "ffmpeg", "-nostdin", "-v", "error", "-i", path, "-map", "0:a:0", "-vn", "-af", filter, "-ar", strconv.Itoa(rate), "-c:a", "pcm_f32le", "-f", "f32le", "pipe:1")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return Loudness{}, err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err = cmd.Start(); err != nil {
		return Loudness{}, fmt.Errorf("start ffmpeg (required for audio decoding): %w", err)
	}
	l, readErr := loudnessFromPCM(stdout, rate, step)
	if readErr != nil {
		_ = cmd.Process.Kill()
	}
	if err = cmd.Wait(); readErr == nil && err != nil {
		return l, fmt.Errorf("ffmpeg: %w: %s", err, stderr.String())
	}
	return l, readErr
}

func loudnessFromPCM(r io.Reader, rate int, step float64) (Loudness, error) {
	n := int(math.Round(step * float64(rate)))
	l := Loudness{Step: float64(n) / float64(rate)}
	br := bufio.NewReader(r)
	buf := make([]byte, 4*n)
	for {
		k, err := io.ReadFull(br, buf)
		if k >= 4 {
			sum := 0.
			for i := 0; i+4 <= k; i += 4 {
				v := float64(math.Float32frombits(binary.LittleEndian.Uint32(buf[i : i+4])))
				sum += v * v
			}
			l.Level = append(l.Level, 10*math.Log10(sum/float64(k/4)+1e-12))
		}
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			break
		}
		if err != nil {
			return l, err
		}
	}
	if len(l.Level) == 0 {
		return l, fmt.Errorf("no audio decoded")
	}
	return l, nil
}

// Rise returns how many dB the median level within +/-half seconds of t
// exceeds the median level of the surrounding +/-context seconds (excluding the
// centre). ok is false when either span lacks enough audio.
func (l Loudness) Rise(t, half, context float64) (float64, bool) {
	var centre, around []float64
	for i, v := range l.Level {
		x := (float64(i) + .5) * l.Step
		switch d := math.Abs(x - t); {
		case d <= half:
			centre = append(centre, v)
		case d <= context:
			around = append(around, v)
		}
	}
	if len(centre)*4 < int(2*half/l.Step) || len(around)*4 < int(2*(context-half)/l.Step) {
		return 0, false
	}
	return median(centre) - medianCopy(around), true
}

func medianCopy(v []float64) float64 {
	c := append([]float64(nil), v...)
	sort.Float64s(c)
	if len(c)%2 == 1 {
		return c[len(c)/2]
	}
	return (c[len(c)/2-1] + c[len(c)/2]) / 2
}
