package acoustic

import (
	"bytes"
	"encoding/binary"
	"math"
	"math/cmplx"
	"testing"
)

func TestFFTAgainstDFT(t *testing.T) {
	input := []complex128{1, 2 + 3i, 4 - 2i, 0, 3, 7i, 2, 1}
	got := append([]complex128{}, input...)
	FFT(got)
	for k := range input {
		var want complex128
		for j, v := range input {
			want += v * cmplx.Rect(1, -2*math.Pi*float64(k*j)/float64(len(input)))
		}
		if cmplx.Abs(got[k]-want) > 1e-10 {
			t.Fatalf("bin %d got %v want %v", k, got[k], want)
		}
	}
}
func TestSilenceWhitening(t *testing.T) {
	o := Defaults()
	s, e := FromPCM(bytes.NewReader(make([]byte, 4*o.Rate*8)), "silence", o)
	if e != nil {
		t.Fatal(e)
	}
	for _, row := range s.Rows {
		for _, v := range row {
			if v != 0 {
				t.Fatalf("silence produced evidence %v", v)
			}
		}
	}
	if len(s.Propose()) != 0 {
		t.Fatal("silence produced events")
	}
}
func TestChirpFrequencyAndFrameTimes(t *testing.T) {
	o := Options{1024, 1024, 256, 50, 200, 0}
	var buf bytes.Buffer
	// Independent emitted chirp f(t)=160-2t Hz over 20 seconds.
	for i := 0; i < 20*o.Rate; i++ {
		x := float64(i) / float64(o.Rate)
		v := float32(math.Sin(2 * math.Pi * (160*x - x*x)))
		if e := binary.Write(&buf, binary.LittleEndian, v); e != nil {
			t.Fatal(e)
		}
	}
	s, e := FromPCM(&buf, "chirp", o)
	if e != nil {
		t.Fatal(e)
	}
	if s.Time(0) != 0.5 || s.Step() != 0.25 {
		t.Fatal("STFT timestamps are not centered")
	}
	for _, frame := range []int{12, 28, 44, 60} {
		want := 160 - 2*s.Time(frame)
		best := 0
		for i, v := range s.Rows[frame] {
			if v > s.Rows[frame][best] {
				best = i
			}
		}
		if math.Abs(s.Hz(best)-want) > 1 {
			t.Fatalf("peak at %v instead of %v", s.Hz(best), want)
		}
	}
	if len(s.Propose()) == 0 {
		t.Fatal("missed clean descending chirp")
	}
}
