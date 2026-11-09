package acoustic

import (
	"math/rand"
	"sort"
	"testing"
)

func TestWhiteningMedianMatchesSortedOrderStatistic(t *testing.T) {
	rng := rand.New(rand.NewSource(71))
	for _, n := range []int{1, 2, 3, 31, 32, 33, 100, 1001, 30000} {
		for pattern := 0; pattern < 5; pattern++ {
			a := make([]float64, n)
			for i := range a {
				switch pattern {
				case 0:
					a[i] = rng.NormFloat64()
				case 1:
					a[i] = float64(i)
				case 2:
					a[i] = -float64(i)
				case 3:
					a[i] = 2
				case 4:
					a[i] = float64(rng.Intn(3))
				}
			}
			ordered := append([]float64(nil), a...)
			sort.Float64s(ordered)
			if got := median(a); got != ordered[n/2] {
				t.Fatal("whitening order statistic changed", n, pattern, got, ordered[n/2])
			}
		}
	}
}
