package scheduler

import (
	"encoding/binary"
	"errors"
	"math"
	"strings"
	"testing"
)

func countRecord(enabled, running, lost uint64) []byte {
	raw := make([]byte, 32)
	binary.LittleEndian.PutUint64(raw[8:], enabled)
	binary.LittleEndian.PutUint64(raw[16:], running)
	binary.LittleEndian.PutUint64(raw[24:], lost)
	return raw
}

func TestCounterFailureNamesOwnedOrdinalWithoutRawDescriptor(t *testing.T) {
	for _, failure := range []struct {
		running, lost uint64
		cause         error
	}{{99, 0, ErrCoverage}, {100, 1, ErrLoss}} {
		read := func(fd int, raw []byte) (int, error) {
			value := countRecord(100, 100, 0)
			if fd == 98765 {
				value = countRecord(100, failure.running, failure.lost)
			}
			return copy(raw, value), nil
		}
		counts, err := readCounters([]int{12345, 98765}, read)
		if !errors.Is(err, failure.cause) || !strings.HasPrefix(err.Error(), "descriptor=1: ") || strings.Contains(err.Error(), "98765") {
			t.Fatal("coverage/loss diagnostic lost its cause or owned ordinal")
		}
		if counts != (Counters{}) {
			t.Fatal("failed coverage returned usable counts")
		}
	}
}

func TestCounterReadsDetectPendingLossAndInactiveTime(t *testing.T) {
	for _, v := range []struct {
		enabled, running, lost uint64
		valid                  bool
	}{{0, 0, 0, true}, {100, 100, 0, true}, {100, 99, 0, false}, {100, 101, 0, false}, {100, 100, 1, false}} {
		_, err := decodeCounters(countRecord(v.enabled, v.running, v.lost))
		if (err == nil) != v.valid {
			t.Fatal(v, err)
		}
		if v.lost > 0 && !errors.Is(err, ErrLoss) {
			t.Fatal("pending loss hidden")
		}
	}
	for _, size := range []int{0, 8, 16, 24, 31, 33} {
		if _, err := decodeCounters(make([]byte, size)); err == nil {
			t.Fatal("wrong read format accepted")
		}
	}
}

func TestAllDescriptorsContributeCoverageAndAnyFailureRejectsIt(t *testing.T) {
	for _, mode := range []string{"valid", "loss", "partial", "error", "overflow"} {
		read := func(fd int, raw []byte) (int, error) {
			if fd == 2 && mode == "error" {
				return 0, ErrObservation
			}
			enabled := uint64(100 * fd)
			lost := uint64(0)
			if fd == 2 && mode == "loss" {
				lost = 1
			}
			if mode == "overflow" {
				enabled = math.MaxUint64
			}
			copy(raw, countRecord(enabled, enabled, lost))
			if fd == 2 && mode == "partial" {
				return 16, nil
			}
			return 32, nil
		}
		result, err := readCounters([]int{1, 2}, read)
		if mode != "valid" {
			if err == nil {
				t.Fatal(mode, "accepted")
			}
			continue
		}
		if err != nil || result.EnabledNanos != 300 || result.RunningNanos != 300 || result.MinimumEnabledNanos != 100 || result.LostSamples != 0 {
			t.Fatal(result, err)
		}
	}
}
