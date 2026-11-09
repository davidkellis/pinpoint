package acoustic

import "testing"

func TestReservedSamplesCannotChangeTrainingNormalization(t *testing.T) {
	a := &Spectrum{SampleRate: 1024, Window: 1024, Hop: 1024, Rows: make([][]float32, 40)}
	b := *a
	b.Rows = make([][]float32, len(a.Rows))
	for i := range a.Rows {
		a.Rows[i] = make([]float32, 30)
		b.Rows[i] = make([]float32, 30)
		for j := range a.Rows[i] {
			a.Rows[i][j] = float32((i*31 + j*17) % 19)
			b.Rows[i][j] = a.Rows[i][j]
			if i >= 20 {
				b.Rows[i][j] += float32(j * 2000)
			}
		}
	}
	train := func(sec float64) bool { return sec < 20 }
	if err := a.whitenTraining(train); err != nil {
		t.Fatal(err)
	}
	if err := b.whitenTraining(train); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		for j := range a.Rows[i] {
			if a.Rows[i][j] != b.Rows[i][j] {
				t.Fatal("reserved audio leaked into training")
			}
		}
	}
}
