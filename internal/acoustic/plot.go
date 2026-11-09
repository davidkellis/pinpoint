package acoustic

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"html"
	"image"
	"image/png"
	"io"
	"os"
	"strings"
)

type Curve struct {
	Label  string
	Points [][2]float64
}

// These are interactive, lossless previews. Faster compression preserves every
// pixel and avoids spending search/report time on smaller PNG containers.
func encodePreview(w io.Writer, im image.Image) error {
	encoder := png.Encoder{CompressionLevel: png.BestSpeed}
	return encoder.Encode(w, im)
}

// SVG writes a self-contained diagnostic figure, including axes and optional
// modeled curves. It can be opened locally without a server or external assets.
func (s *Spectrum) SVG(path string, curves []Curve) error {
	var bitmap bytes.Buffer
	if err := encodePreview(&bitmap, s.plotImage()); err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" width="1280" height="720" viewBox="0 0 1280 720"><rect width="1280" height="720" fill="#111827"/><g fill="#e5e7eb" font-family="sans-serif" font-size="14">`)
	fmt.Fprintf(&b, `<text x="80" y="30">Whitened spectrogram — %s</text>`, html.EscapeString(s.Source))
	fmt.Fprintf(&b, `<image x="80" y="55" width="1140" height="540" preserveAspectRatio="none" href="data:image/png;base64,%s"/>`, base64.StdEncoding.EncodeToString(bitmap.Bytes()))
	t0, t1 := s.Time(0), s.Time(len(s.Rows)-1)
	f0, f1 := s.Hz(0), s.Hz(len(s.Rows[0])-1)
	for i := 0; i <= 8; i++ {
		u := float64(i) / 8
		fmt.Fprintf(&b, `<text x="%.1f" y="620" text-anchor="middle">%.1f</text><text x="70" y="%.1f" text-anchor="end">%.0f</text>`, 80+1140*u, t0+u*(t1-t0), 600-540*u, f0+u*(f1-f0))
	}
	b.WriteString(`<text x="650" y="650" text-anchor="middle">Audio time (seconds)</text><text x="25" y="325" transform="rotate(-90 25 325)" text-anchor="middle">Frequency (Hz)</text>`)
	colors := []string{"#5eead4", "#93c5fd", "#e879f9", "#fda4af"}
	for i, c := range curves {
		var points strings.Builder
		for _, p := range c.Points {
			if p[0] >= t0 && p[0] <= t1 && p[1] >= f0 && p[1] <= f1 {
				fmt.Fprintf(&points, "%.2f,%.2f ", 80+1140*(p[0]-t0)/(t1-t0), 595-540*(p[1]-f0)/(f1-f0))
			}
		}
		color := colors[i%len(colors)]
		fmt.Fprintf(&b, `<polyline points="%s" fill="none" stroke="%s" stroke-width="1.3" opacity="0.85"><title>%s</title></polyline>`, points.String(), color, html.EscapeString(c.Label))
	}
	b.WriteString(`<text x="80" y="690">Brightness: 0–30 dB above estimated background. Hover over a colored curve for its source.</text></g></svg>`)
	return os.WriteFile(path, []byte(b.String()), 0644)
}
