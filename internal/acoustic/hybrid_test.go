package acoustic

import (
	"math"
	"testing"
)

func TestBlockOriginPreservesMeasuredCurveAndQuality(t *testing.T) {
	s := shortSpectrum(false)
	a, err := s.DiscoverProfile(ShortPassProfile)
	if err != nil || a.Strong == 0 {
		t.Fatal("missing independent physical fixture")
	}
	s.TimeOffset = 960
	b, err := s.DiscoverProfile(ShortPassProfile)
	if err != nil || a.Strong != b.Strong || len(a.Events) != len(b.Events) {
		t.Fatal("block origin changed curve classification", err)
	}
	for i, e := range a.Events {
		v := b.Events[i]
		if math.Abs(v.Start-e.Start-960) > 1e-6 || math.Abs(v.End-e.End-960) > 1e-6 || len(v.Ridge) != len(e.Ridge) {
			t.Fatal("lost original recording offsets")
		}
		for k, p := range e.Ridge {
			if math.Abs(v.Ridge[k].Time-p.Time-960) > 1e-6 || p.Hz != v.Ridge[k].Hz {
				t.Fatal("changed observed frequencies/times")
			}
		}
	}
}

func TestHybridPreservesCurvesFromBothProfilesWithoutInventingPaths(t *testing.T) {
	s := shortSpectrum(false)
	d, err := s.DiscoverProfile(ShortPassProfile)
	if err != nil || d.Strong == 0 {
		t.Fatal("fixture", err)
	}
	var e Event
	for _, v := range d.Events {
		if v.Evidence.Level == "strong" {
			e = v
			break
		}
	}
	other := e
	other.Start += 100
	other.End += 100
	other.CPATime += 100
	other.Ridge = append([]RidgeSample(nil), e.Ridge...)
	for i := range other.Ridge {
		other.Ridge[i].Time += 100
	}
	r := MergeProfiles(DetectionReport{Events: []Event{e}, ScannedEnd: 120}, DetectionReport{Events: []Event{e, other}, ScannedEnd: 220})
	if len(r.Events) != 2 || r.Strong != 2 || r.ScannedEnd != 220 || r.Rejected["duplicate_across_profiles"] != 1 {
		t.Fatal("lost or duplicated measured curves")
	}
	for i, v := range []Event{e, other} {
		for k, p := range v.Ridge {
			if p != r.Events[i].Ridge[k] {
				t.Fatal("synthesized replacement ridge")
			}
		}
	}
}
