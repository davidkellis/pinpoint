package acoustic

import (
	"math"
	"testing"
)

func TestExtensionFollowsMeasuredCurveAcrossShortMask(t *testing.T) {
	s := shortSpectrum(false)
	s.discoveryProfile = ShortPassProfile
	var truth, seed []ridgePoint
	for frame, row := range s.Rows {
		bin := 0
		for k := 1; k < 1500; k++ {
			if row[k] > row[bin] {
				bin = k
			}
		}
		p := ridgePoint{frame, s.Hz(bin), float64(row[bin])}
		truth = append(truth, p)
		if frame >= 35 && frame <= 85 {
			seed = append(seed, p)
		}
	}
	// Brief noise masks the ridge. A second louder stationary tone crosses
	// the extension: momentum and future path evidence must prevent a switch.
	for frame, row := range s.Rows {
		if frame >= 95 && frame <= 98 {
			clear(row)
			continue
		}
		if frame < 35 {
			for k := range row {
				d := (s.Hz(k) - 2650) / 2
				row[k] += float32(40 * math.Exp(-d*d))
			}
		}
	}
	got := extendRidge(s, seed)
	if len(got) < 110 || got[0].frame > 2 || got[len(got)-1].frame < 118 {
		t.Fatalf("truncated extension: %d points, %d..%d", len(got), got[0].frame, got[len(got)-1].frame)
	}
	for i, p := range got {
		if i > 0 && got[i-1].frame >= p.frame {
			t.Fatal("unordered or duplicated observations")
		}
		if p.frame >= 95 && p.frame <= 98 {
			t.Fatal("invented observation inside mask")
		}
		if math.Abs(p.hz-truth[p.frame].hz) > 4 {
			t.Fatalf("switched ridge at %d: %g vs %g", p.frame, p.hz, truth[p.frame].hz)
		}
	}
}

func TestExtensionStopsAtUnsupportedGapAndDoesNotPromoteLinearSweep(t *testing.T) {
	s := shortSpectrum(false)
	s.discoveryProfile = ShortPassProfile
	var seed []ridgePoint
	for frame := 35; frame <= 85; frame++ {
		row := s.Rows[frame]
		bin := 0
		for k := 1; k < 1500; k++ {
			if row[k] > row[bin] {
				bin = k
			}
		}
		seed = append(seed, ridgePoint{frame, s.Hz(bin), float64(row[bin])})
	}
	for frame := 90; frame <= 110; frame++ {
		clear(s.Rows[frame])
	}
	got := extendRidge(s, seed)
	if got[len(got)-1].frame >= 90 {
		t.Fatal("bridged unsupported long gap")
	}
	u := shortSpectrum(true)
	u.discoveryProfile = ShortPassProfile
	d, err := u.DiscoverProfile(ShortPassProfile)
	if err != nil {
		t.Fatal(err)
	}
	extended := extendCandidates(u, d)
	if extended.Strong != 0 || extended.Joint != 0 {
		t.Fatal("promoted generic pitch sweep")
	}
}
