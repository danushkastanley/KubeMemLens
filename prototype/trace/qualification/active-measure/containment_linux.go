//go:build linux

package main

import (
	"errors"
	"os"
	"strings"
)

func (g *group) containment() (*containmentSample, error) {
	raw, err := g.read("cpu.max")
	if err != nil {
		return nil, err
	}
	cpu, err := parseCPULimit(raw)
	if err != nil {
		return nil, err
	}
	sample := &containmentSample{CPU: cpu}
	for _, field := range []struct {
		name  string
		parse func(string) (observedLimit, error)
		out   *observedLimit
	}{
		{"cpu.max.burst", numericObservation, &sample.CPUBurst},
		{"memory.max", maximumObservation, &sample.MemoryMax},
		{"memory.peak", numericObservation, &sample.MemoryPeak},
		{"pids.max", maximumObservation, &sample.PIDsMax},
	} {
		raw, err := g.read(field.name)
		if errors.Is(err, os.ErrNotExist) {
			*field.out = observedLimit{State: "unavailable"}
			continue
		}
		if err != nil {
			return nil, err
		}
		*field.out, err = field.parse(strings.TrimSpace(string(raw)))
		if err != nil {
			return nil, err
		}
	}
	// The same cgroup lifetime must surround both ordinary and limit reads.
	return sample, g.validate()
}
