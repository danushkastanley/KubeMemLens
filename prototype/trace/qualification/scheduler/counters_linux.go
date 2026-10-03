package scheduler

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"

	"golang.org/x/sys/unix"
)

var ErrCoverage = errors.New("scheduler perf event was not continuously running")

// Counters retains numeric perf coverage evidence without event/task identities.
// Enabled and running totals are sums over all owned per-CPU descriptors.
type Counters struct {
	EnabledNanos        uint64 `json:"enabledNanos"`
	RunningNanos        uint64 `json:"runningNanos"`
	LostSamples         uint64 `json:"lostSamples"`
	MinimumEnabledNanos uint64 `json:"minimumEnabledNanos"`
}

func decodeCounters(raw []byte) (Counters, error) {
	if len(raw) != 32 {
		return Counters{}, ErrObservation
	}
	enabled := binary.LittleEndian.Uint64(raw[8:16])
	running := binary.LittleEndian.Uint64(raw[16:24])
	lost := binary.LittleEndian.Uint64(raw[24:32])
	if lost != 0 {
		return Counters{}, fmt.Errorf("%w: lost=%d", ErrLoss, lost)
	}
	if enabled != running {
		return Counters{}, fmt.Errorf("%w: enabled=%d running=%d", ErrCoverage, enabled, running)
	}
	return Counters{enabled, running, lost, enabled}, nil
}

func readCounters(fds []int, read func(int, []byte) (int, error)) (Counters, error) {
	result := Counters{MinimumEnabledNanos: math.MaxUint64}
	if len(fds) == 0 || len(fds) > 256 {
		return Counters{}, ErrObservation
	}
	for ordinal, fd := range fds {
		var raw [32]byte
		n, err := read(fd, raw[:])
		if err != nil || n != len(raw) {
			return Counters{}, ErrObservation
		}
		next, err := decodeCounters(raw[:])
		if err != nil {
			if errors.Is(err, ErrCoverage) {
				err = coverageDiagnostic(err, fd, read)
			}
			return Counters{}, fmt.Errorf("descriptor=%d: %w", ordinal, err)
		}
		if next.EnabledNanos > math.MaxUint64-result.EnabledNanos || next.RunningNanos > math.MaxUint64-result.RunningNanos {
			return Counters{}, ErrObservation
		}
		result.EnabledNanos += next.EnabledNanos
		result.RunningNanos += next.RunningNanos
		if next.MinimumEnabledNanos < result.MinimumEnabledNanos {
			result.MinimumEnabledNanos = next.MinimumEnabledNanos
		}
	}
	return result, nil
}

// The rejected reading remains decisive. One numeric follow-up helps investigate
// intermittent coverage failures without retrying or replacing an observation.
func coverageDiagnostic(cause error, fd int, read func(int, []byte) (int, error)) error {
	var raw [32]byte
	n, err := read(fd, raw[:])
	if err != nil || n != len(raw) {
		return fmt.Errorf("%w; follow_read_bytes=%d follow_read_failed=%t", cause, n, err != nil)
	}
	return fmt.Errorf("%w; follow_enabled=%d follow_running=%d follow_lost=%d", cause,
		binary.LittleEndian.Uint64(raw[8:16]), binary.LittleEndian.Uint64(raw[16:24]),
		binary.LittleEndian.Uint64(raw[24:32]))
}

// CheckCounters detects losses even when the kernel has not emitted a LOST
// record into a ring yet. PERF_FORMAT_LOST requires Linux 6.0 or newer; an older
// kernel must fail setup rather than silently omit this integrity check.
func (c *Capture) CheckCounters() (Counters, error) {
	if c.closed || c.failed {
		return Counters{}, ErrObservation
	}
	result, err := readCounters(c.fds, unix.Read)
	if err == nil && result.MinimumEnabledNanos == 0 {
		err = ErrObservation
	}
	if err != nil {
		c.failed = true
	}
	return result, err
}
