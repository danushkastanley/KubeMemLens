package resourcemetrics

import (
	"math"
	"testing"
)

func FuzzUsageValue(f *testing.F) {
	for _, seed := range []string{"0", "1n", "125m", "32Mi", "1e18", "1e-18", "1e2147483647", "-1", "NaN", ""} {
		f.Add(seed, true)
	}
	f.Fuzz(func(t *testing.T, value string, cpu bool) {
		if len(value) > 256 {
			return
		}
		scale := 0
		if cpu {
			scale = 9
		}
		result, ok := usageValue(value, scale)
		if ok && result > math.MaxInt64 {
			t.Fatal("accepted quantity overflowed the declared range")
		}
		if !ok && result != 0 {
			t.Fatal("rejected quantity retained a value")
		}
	})
}
