// Package oomtrace relates victim-scoped OOM observations to fixed cgroup files.
// It neither loads programmes nor treats cgroup counters as global OOM decisions.
package oomtrace

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/cgroup"
	"github.com/danushkastanley/kube-memlens/internal/trace"
)

var ErrSample = errors.New("invalid OOM cgroup sample")

// RawSample contains only fixed files read through the retained cgroup handle.
// Nil means that individual file is unavailable, never a measured zero value.
type RawSample struct {
	Local, Hierarchical, Current, Limit, Pressure []byte
}

func (RawSample) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "[OOM sample input]") }
func (RawSample) MarshalJSON() ([]byte, error) { return nil, ErrSample }

type Sample struct {
	target              trace.TargetIdentity
	start, end          time.Time
	local, hierarchical map[string]uint64
	current, some, full *uint64
	limit               trace.OOMLimit
}

func (Sample) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "[ephemeral OOM sample]") }
func (Sample) MarshalJSON() ([]byte, error) { return nil, ErrSample }

func NewSample(target trace.TargetIdentity, start, end time.Time, raw RawSample) (Sample, error) {
	if target.ValidateLifetime() != nil || target.CgroupID == 0 || start.IsZero() || start.Before(target.ContainerStartedAt) || end.Before(start) || end.Sub(start) > time.Second {
		return Sample{}, ErrSample
	}
	present := 0
	for _, data := range [][]byte{raw.Local, raw.Hierarchical, raw.Current, raw.Limit, raw.Pressure} {
		if len(data) > 16384 {
			return Sample{}, ErrSample
		}
		if len(data) != 0 {
			present++
		}
	}
	if present == 0 {
		return Sample{}, ErrSample
	}
	target.ContainerStartedAt = target.ContainerStartedAt.UTC()
	sample := Sample{target: target, start: start.UTC(), end: end.UTC(), limit: trace.OOMLimit{State: "unreported"}}
	var err error
	sample.local, err = parseEvents(raw.Local)
	if err != nil {
		return Sample{}, err
	}
	sample.hierarchical, err = parseEvents(raw.Hierarchical)
	if err != nil {
		return Sample{}, err
	}
	if len(raw.Current) != 0 {
		value, err := cgroup.ParseMemoryCurrent(raw.Current)
		if err != nil {
			return Sample{}, ErrSample
		}
		sample.current = &value
	}
	if len(raw.Limit) != 0 {
		sample.limit, err = parseLimit(raw.Limit)
		if err != nil {
			return Sample{}, err
		}
	}
	if len(raw.Pressure) != 0 {
		pressure, err := cgroup.ParseMemoryPressure(raw.Pressure)
		if err != nil {
			return Sample{}, ErrSample
		}
		sample.some, sample.full = &pressure.Some.TotalMicros, &pressure.Full.TotalMicros
	}
	return sample, nil
}

func parseEvents(data []byte) (map[string]uint64, error) {
	if len(data) == 0 {
		return nil, nil
	}
	events, err := cgroup.ParseMemoryEvents(data)
	if err != nil {
		return nil, ErrSample
	}
	return events.Values, nil
}

func parseLimit(data []byte) (trace.OOMLimit, error) {
	if strings.TrimSpace(string(data)) == "max" {
		return trace.OOMLimit{State: "unlimited"}, nil
	}
	value, err := cgroup.ParseMemoryCurrent(data)
	if err != nil {
		return trace.OOMLimit{}, ErrSample
	}
	return trace.OOMLimit{State: "finite", Bytes: &value}, nil
}
