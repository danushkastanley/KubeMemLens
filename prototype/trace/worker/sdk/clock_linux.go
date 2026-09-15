package sdk

import (
	"math"
	"runtime"
	"time"

	"github.com/cilium/ebpf"
	"github.com/danushkastanley/kube-memlens/internal/trace"
	"github.com/danushkastanley/kube-memlens/prototype/trace/filecache"
	"golang.org/x/sys/unix"
)

// Initial reservations are larger than the fixed ELF map payloads. Actual
// owned allocations are checked before attachment; a profile that exceeds its
// reservation fails qualification rather than silently raising the budget.
func validateProfile(spec trace.Specification) error {
	minimum := uint64(8 << 20)
	if spec.Kind() == trace.Cache {
		minimum = 4 << 20
	}
	if spec.Bounds().MapBytes < minimum {
		return ErrWorker
	}
	var system unix.Utsname
	if unix.Uname(&system) != nil {
		return ErrWorker
	}
	machine := unix.ByteSliceToString(system.Machine[:])
	if !((runtime.GOARCH == "arm64" && machine == "aarch64") || (runtime.GOARCH == "amd64" && machine == "x86_64")) {
		return ErrWorker
	}
	cpus, err := ebpf.PossibleCPU()
	if err != nil || cpus <= 0 || cpus > 128 {
		return ErrWorker
	}
	page := unix.Getpagesize()
	if page != 4096 && page != 65536 {
		return ErrWorker
	}
	return nil
}

func monotonicNS() (uint64, error) {
	var now unix.Timespec
	if unix.ClockGettime(unix.CLOCK_MONOTONIC, &now) != nil || now.Sec < 0 || now.Sec > math.MaxInt64/int64(time.Second) || now.Nsec < 0 || now.Nsec >= int64(time.Second) {
		return 0, ErrWorker
	}
	return uint64(now.Sec)*uint64(time.Second) + uint64(now.Nsec), nil
}

func align(duration time.Duration) (filecache.Alignment, error) {
	before, err := monotonicNS()
	if err != nil {
		return filecache.Alignment{}, err
	}
	wall := time.Now().UTC()
	after, err := monotonicNS()
	if err != nil || after < before || after-before > uint64(10*time.Millisecond) {
		return filecache.Alignment{}, ErrWorker
	}
	uncertainty := time.Duration((after-before)/2 + 1)
	return filecache.Alignment{MonotonicNS: before + (after-before)/2, WallTime: wall, Duration: duration, Uncertainty: uncertainty}, nil
}

func alignDeadline(deadline time.Time) (filecache.Alignment, error) {
	clock, err := align(time.Until(deadline))
	// Choose the earlier end of the alignment uncertainty interval. Computing
	// duration before sampling the clocks would extend the kernel deadline by
	// the time spent sampling them.
	clock.Duration = deadline.Sub(clock.WallTime) - clock.Uncertainty
	if err != nil || clock.Duration <= 0 || clock.Duration > 5*time.Minute {
		return filecache.Alignment{}, ErrWorker
	}
	return clock, nil
}

func observationWindow(clock filecache.Alignment, started, stopped uint64) (time.Time, time.Time) {
	if clock.Duration <= 0 || clock.MonotonicNS > math.MaxUint64-uint64(clock.Duration) || started < clock.MonotonicNS || stopped < started {
		return time.Time{}, time.Time{}
	}
	end := clock.MonotonicNS + uint64(clock.Duration)
	if started >= end {
		return time.Time{}, time.Time{}
	}
	if stopped > end {
		stopped = end
	}
	return clock.WallTime.Add(time.Duration(started - clock.MonotonicNS)), clock.WallTime.Add(time.Duration(stopped - clock.MonotonicNS))
}

func alignmentUncertainty(start filecache.Alignment) (time.Duration, error) {
	end, err := align(start.Duration)
	if err != nil || end.MonotonicNS < start.MonotonicNS || end.MonotonicNS-start.MonotonicNS > uint64(6*time.Minute) {
		return 0, ErrWorker
	}
	expected := start.WallTime.Add(time.Duration(end.MonotonicNS - start.MonotonicNS))
	drift := end.WallTime.Sub(expected)
	if drift < -5*time.Millisecond || drift > 5*time.Millisecond {
		return 0, ErrWorker
	}
	if drift < 0 {
		drift = -drift
	}
	uncertainty := start.Uncertainty + end.Uncertainty + drift
	if uncertainty > 5*time.Millisecond {
		return 0, ErrWorker
	}
	return uncertainty, nil
}
