package geo

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
)

// HeightSampler returns receiver ellipsoidal heights (SRTM terrain + geoid +
// microphone height) safely from many goroutines. Terrain voids fall back to
// sea level and are counted. Missing tiles are downloaded only when allowed.
type HeightSampler struct {
	mu       sync.Mutex
	terrain  *Terrain
	geoid    *Geoid
	mic      float64
	download bool
	voids    int
}

func NewHeightSampler(terrainDir, geoidPath string, mic float64, download bool) (*HeightSampler, error) {
	g, err := LoadGeoid(geoidPath)
	if err != nil {
		return nil, err
	}
	if !Finite(mic) || mic < 0 || mic > 500 {
		return nil, fmt.Errorf("invalid microphone height")
	}
	return &HeightSampler{terrain: &Terrain{Dir: terrainDir}, geoid: g, mic: mic, download: download}, nil
}

// Voids reports how many samples fell on terrain voids.
func (h *HeightSampler) Voids() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.voids
}

// Func adapts the sampler to func(lat, lon) for search options.
func (h *HeightSampler) Func(ctx context.Context) func(lat, lon float64) (float64, error) {
	return func(lat, lon float64) (float64, error) {
		n, err := h.geoid.Height(lat, lon)
		if err != nil {
			return 0, err
		}
		h.mu.Lock()
		defer h.mu.Unlock()
		t, err := h.terrain.Height(lat, lon)
		if err != nil && os.IsNotExist(err) && h.download {
			if err = DownloadTerrain(ctx, h.terrain.Dir, TileName(lat, lon)); err != nil {
				return 0, err
			}
			t, err = h.terrain.Height(lat, lon)
		}
		if err != nil && strings.Contains(err.Error(), "terrain void") {
			h.voids++
			t, err = 0, nil
		}
		if err != nil {
			return 0, err
		}
		return t + n + h.mic, nil
	}
}
