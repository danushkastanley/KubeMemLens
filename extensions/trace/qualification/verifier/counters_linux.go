package verifier

import (
	"encoding/binary"
	"math"

	"golang.org/x/sys/unix"
)

type PerfCounters struct {
	Events              uint64 `json:"events"`
	EnabledNanos        uint64 `json:"enabledNanos"`
	RunningNanos        uint64 `json:"runningNanos"`
	LostSamples         uint64 `json:"lostSamples"`
	MinimumEnabledNanos uint64 `json:"minimumEnabledNanos"`
	Descriptors         int    `json:"descriptors"`
}
type descriptorCount struct{ events, enabled, running, lost uint64 }

func decodeCounters(raw []byte) (descriptorCount, error) {
	if len(raw) != 32 {
		return descriptorCount{}, ErrObservation
	}
	c := descriptorCount{binary.LittleEndian.Uint64(raw[0:8]), binary.LittleEndian.Uint64(raw[8:16]),
		binary.LittleEndian.Uint64(raw[16:24]), binary.LittleEndian.Uint64(raw[24:32])}
	// Time is the cgroup context's scheduled time, not elapsed wall time. A
	// CPU on which this cgroup never ran can legitimately report all zeroes.
	if c.lost != 0 || c.running != c.enabled || (c.events > 0 && c.enabled == 0) {
		return descriptorCount{}, ErrObservation
	}
	return c, nil
}

func readDescriptor(fd int) (descriptorCount, error) {
	var raw [32]byte
	for attempt := 0; attempt < 3; attempt++ {
		n, err := unix.Read(fd, raw[:])
		if err == unix.EINTR {
			continue
		}
		if err != nil || n != len(raw) {
			return descriptorCount{}, ErrObservation
		}
		return decodeCounters(raw[:])
	}
	return descriptorCount{}, ErrObservation
}

func (c *Capture) Counters() (PerfCounters, error) {
	if c.closed || c.failed || len(c.fds) == 0 {
		return PerfCounters{}, ErrObservation
	}
	result := PerfCounters{MinimumEnabledNanos: math.MaxUint64, Descriptors: len(c.fds)}
	for index, fd := range c.fds {
		value, err := readDescriptor(fd)
		old := c.counts[index]
		if err != nil || value.events < old.events || value.enabled < old.enabled || value.running < old.running {
			c.failed = true
			return PerfCounters{}, ErrObservation
		}
		c.counts[index] = value
		for _, sum := range []struct {
			target *uint64
			value  uint64
		}{
			{&result.Events, value.events}, {&result.EnabledNanos, value.enabled},
			{&result.RunningNanos, value.running}, {&result.LostSamples, value.lost},
		} {
			if math.MaxUint64-*sum.target < sum.value {
				c.failed = true
				return PerfCounters{}, ErrObservation
			}
			*sum.target += sum.value
		}
		result.MinimumEnabledNanos = min(result.MinimumEnabledNanos, value.enabled)
	}
	return result, nil
}
