package verifier

import (
	"encoding/binary"
	"testing"
)

func counterBytes(events, enabled, running, lost uint64) []byte {
	raw := make([]byte, 32)
	for index, value := range []uint64{events, enabled, running, lost} {
		binary.LittleEndian.PutUint64(raw[index*8:], value)
	}
	return raw
}

func TestCgroupCounterReaderAllowsIdleCPUsButNotLossOrInactiveCoverage(t *testing.T) {
	for _, raw := range [][]byte{counterBytes(0, 0, 0, 0), counterBytes(10, 100, 100, 0)} {
		if _, err := decodeCounters(raw); err != nil {
			t.Fatal("valid cgroup accounting rejected")
		}
	}
	for _, raw := range [][]byte{nil, make([]byte, 31), make([]byte, 33),
		counterBytes(1, 0, 0, 0), counterBytes(1, 100, 90, 0), counterBytes(1, 90, 100, 0), counterBytes(1, 100, 100, 1)} {
		if _, err := decodeCounters(raw); err == nil {
			t.Fatal("incomplete perf accounting accepted")
		}
	}
}
