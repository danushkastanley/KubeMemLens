package cgroup

import (
	"fmt"
	"math"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/model"
)

const maxFuzzCgroupBytes = 16 << 10

func FuzzParseCgroupKeyValues(f *testing.F) {
	for _, seed := range [][]byte{
		[]byte("anon 100\nfile 200\n"),
		[]byte("anon 18446744073709551615\nfuture_key 42\n"),
		[]byte("  anon\t1\n\nfuture_key 2  \n"),
		[]byte("anon nope\n"),
		[]byte("missing-value\n"),
		[]byte("zswap 4\nzswapped 12\noom_group_kill 1\n"),
		[]byte("zswap 4\noom_kill 1\n"),
		{},
	} {
		f.Add(seed)
	}

	known := map[string]struct{}{
		"anon": {},
		"file": {},
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > maxFuzzCgroupBytes {
			return
		}
		values, unknown, err := parseKeyValues(data, known)
		if err != nil {
			return
		}
		if values == nil || unknown == nil {
			t.Fatal("successful parse returned a nil map")
		}
		for key := range values {
			if _, ok := known[key]; !ok {
				t.Fatalf("known values contain unknown key %q", key)
			}
			if _, ok := unknown[key]; ok {
				t.Fatalf("key %q appears in both result maps", key)
			}
		}
		for key := range unknown {
			if _, ok := known[key]; ok {
				t.Fatalf("unknown values contain known key %q", key)
			}
		}
		assertSchema8ValuesKnownOnlyWhenReported(t, data)
	})
}

// The same bytes read as memory.stat and as memory.events: zswap is known only
// when both of its keys are present, oom_group_kill only when its key is, and
// an unknown value is zero.
func assertSchema8ValuesKnownOnlyWhenReported(t *testing.T, data []byte) {
	t.Helper()
	if stat, err := ParseMemoryStat(data); err == nil {
		zswap := zswapFromStat(stat)
		_, pool := stat.Values["zswap"]
		_, swapped := stat.Values["zswapped"]
		if zswap.Known != (pool && swapped) || (!zswap.Known && zswap != (model.Zswap{})) {
			t.Fatalf("zswap %#v from keys present=%t/%t", zswap, pool, swapped)
		}
	}
	if events, err := ParseMemoryEvents(data); err == nil {
		value, known := groupKillEvents(events.Values)
		_, present := events.Values["oom_group_kill"]
		if known != present || (!known && value != 0) {
			t.Fatalf("oom_group_kill %d known=%t from key present=%t", value, known, present)
		}
	}
}

func FuzzParseMemoryPressure(f *testing.F) {
	for _, seed := range [][]byte{
		[]byte("some avg10=1.25 avg60=0.50 avg300=0.10 total=1234\nfull avg10=0.05 avg60=0.01 avg300=0.00 total=45\n"),
		[]byte("full total=45 avg300=0 avg60=0.01 avg10=0.05\nsome total=1234 avg300=0.10 avg60=0.50 avg10=1.25\n"),
		[]byte("some avg10=0 avg60=0 avg300=0 total=18446744073709551615\nfull avg10=0 avg60=0 avg300=0 total=0\n"),
		[]byte("some avg10=0 avg60=0 avg300=0 total=0\n"),
		[]byte("unknown avg10=0 avg60=0 avg300=0 total=0\n"),
		[]byte("some avg10=NaN avg60=+Inf avg300=100.01 total=0\nfull avg10=-1 avg60=0 avg300=0 total=0\n"),
		{},
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > maxFuzzCgroupBytes {
			return
		}
		pressure, err := ParseMemoryPressure(data)
		if err != nil {
			return
		}

		for _, sample := range []pressureSample{pressure.Some, pressure.Full} {
			for _, average := range []float64{sample.Avg10, sample.Avg60, sample.Avg300} {
				if !(average >= 0 && average <= 100) {
					t.Fatalf("accepted pressure average %v outside 0 to 100", average)
				}
			}
		}

		canonical := []byte(fmt.Sprintf(
			"some avg10=%g avg60=%g avg300=%g total=%d\nfull avg10=%g avg60=%g avg300=%g total=%d\n",
			pressure.Some.Avg10,
			pressure.Some.Avg60,
			pressure.Some.Avg300,
			pressure.Some.TotalMicros,
			pressure.Full.Avg10,
			pressure.Full.Avg60,
			pressure.Full.Avg300,
			pressure.Full.TotalMicros,
		))
		reparsed, err := ParseMemoryPressure(canonical)
		if err != nil {
			t.Fatalf("canonical pressure failed to parse: %v", err)
		}
		assertPressureSampleEqual(t, "some", pressure.Some, reparsed.Some)
		assertPressureSampleEqual(t, "full", pressure.Full, reparsed.Full)
	})
}

func assertPressureSampleEqual(t *testing.T, class string, want, got pressureSample) {
	t.Helper()
	if math.Float64bits(got.Avg10) != math.Float64bits(want.Avg10) ||
		math.Float64bits(got.Avg60) != math.Float64bits(want.Avg60) ||
		math.Float64bits(got.Avg300) != math.Float64bits(want.Avg300) ||
		got.TotalMicros != want.TotalMicros {
		t.Fatalf("%s pressure changed after canonical round trip: got %#v, want %#v", class, got, want)
	}
}
