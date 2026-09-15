package sdk

import (
	"math"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/prototype/trace/filecache"
)

func TestObservationWindowExcludesPreparationAndLateTeardown(t *testing.T) {
	wall := time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)
	clock := filecache.Alignment{MonotonicNS: uint64(time.Second), WallTime: wall, Duration: 30 * time.Second}
	started, ended := observationWindow(clock, uint64(3*time.Second), uint64(40*time.Second))
	if !started.Equal(wall.Add(2*time.Second)) || !ended.Equal(wall.Add(30*time.Second)) {
		t.Fatal("preparation or teardown included in observation window")
	}
	_, ended = observationWindow(clock, uint64(3*time.Second), uint64(4*time.Second))
	if !ended.Equal(wall.Add(3 * time.Second)) {
		t.Fatal("early cancellation extended to deadline")
	}
}

func TestInvalidMonotonicIntervalsAreUnknown(t *testing.T) {
	clock := filecache.Alignment{MonotonicNS: 100, WallTime: time.Now().UTC(), Duration: time.Second}
	for _, pair := range [][2]uint64{{99, 100}, {102, 101}, {uint64(2 * time.Second), uint64(3 * time.Second)}} {
		start, end := observationWindow(clock, pair[0], pair[1])
		if !start.IsZero() || !end.IsZero() {
			t.Fatal("invalid interval reported")
		}
	}
	clock.MonotonicNS = math.MaxUint64
	start, end := observationWindow(clock, math.MaxUint64, math.MaxUint64)
	if !start.IsZero() || !end.IsZero() {
		t.Fatal("overflow accepted")
	}
}

func TestAlignmentHonoursAbsoluteDeadline(t *testing.T) {
	deadline := time.Now().Add(time.Second)
	clock, err := alignDeadline(deadline)
	if err != nil {
		t.Fatal(err)
	}
	if clock.WallTime.Add(clock.Duration + clock.Uncertainty).After(deadline) {
		t.Fatal("kernel deadline extended by clock sampling")
	}
	if _, err := alignDeadline(time.Now().Add(-time.Second)); err == nil {
		t.Fatal("expired session accepted")
	}
}
