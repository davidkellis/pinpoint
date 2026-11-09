package geo

import (
	"compress/gzip"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

func TileName(lat, lon float64) string {
	la, lo := int(math.Floor(lat)), int(math.Floor(lon))
	ns, ew := "N", "E"
	if la < 0 {
		ns = "S"
	}
	if lo < 0 {
		ew = "W"
	}
	return fmt.Sprintf("%s%02d%s%03d.hgt", ns, int(math.Abs(float64(la))), ew, int(math.Abs(float64(lo))))
}

// DownloadTerrain caches Mapzen Skadi EGM96 terrain. Resample the source to
// three arcseconds to keep regional searches bounded in memory. Negative ocean
// bathymetry is set to sea level; terrain voids remain voids and fail sampling.
func DownloadTerrain(ctx context.Context, dir, name string) error {
	path := filepath.Join(dir, name)
	if st, e := os.Stat(path); e == nil && st.Size() == 1201*1201*2 {
		return nil
	}
	url := "https://s3.amazonaws.com/elevation-tiles-prod/skadi/" + name[:3] + "/" + name + ".gz"
	b, e := download(ctx, url, 64<<20)
	if e != nil {
		return e
	}
	if len(b) != 3601*3601*2 {
		return fmt.Errorf("unexpected terrain tile size for %s", name)
	}
	out := make([]byte, 1201*1201*2)
	for y := 0; y < 1201; y++ {
		for x := 0; x < 1201; x++ {
			i := 2 * (y*3*3601 + x*3)
			v := int16(binary.BigEndian.Uint16(b[i : i+2]))
			if v < 0 && v != -32768 {
				v = 0
			}
			binary.BigEndian.PutUint16(out[2*(y*1201+x):], uint16(v))
		}
	}
	return cacheFile(path, out)
}

func DownloadGeoid(ctx context.Context, dir string) (string, error) {
	path := filepath.Join(dir, "egm96_15.gtx")
	if _, e := LoadGeoid(path); e == nil {
		return path, nil
	}
	b, e := download(ctx, "https://raw.githubusercontent.com/OSGeo/PROJ-datumgrid/master/egm96_15.gtx", 8<<20)
	if e != nil {
		return "", e
	}
	if e = cacheFile(path, b); e != nil {
		return "", e
	}
	_, e = LoadGeoid(path)
	return path, e
}

func cacheFile(path string, b []byte) error {
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(path), ".download-*")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	_, e = f.Write(b)
	ce := f.Close()
	if e != nil {
		return e
	}
	if ce != nil {
		return ce
	}
	return os.Rename(f.Name(), path)
}

func download(ctx context.Context, url string, limit int64) ([]byte, error) {
	req, e := http.NewRequestWithContext(ctx, "GET", url, nil)
	if e != nil {
		return nil, e
	}
	res, e := (&http.Client{Timeout: 90 * time.Second}).Do(req)
	if e != nil {
		return nil, e
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, fmt.Errorf("elevation download HTTP %d: %s", res.StatusCode, url)
	}
	var r io.Reader = res.Body
	if filepath.Ext(url) == ".gz" {
		g, e := gzip.NewReader(r)
		if e != nil {
			return nil, e
		}
		defer g.Close()
		r = g
	}
	b, e := io.ReadAll(io.LimitReader(r, limit+1))
	if e == nil && int64(len(b)) > limit {
		e = fmt.Errorf("elevation download exceeds size limit")
	}
	return b, e
}
