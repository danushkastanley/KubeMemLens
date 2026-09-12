package cgroup

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/danushkastanley/kube-memlens/internal/model"
)

const maxIOPressureBytes = 4096

// I/O enrichment failure must not discard an otherwise valid memory sample.
// Missing files do not establish whether PSI is disabled or unsupported.
func readIOPressure(dir string) model.IOPressure {
	file, err := os.Open(filepath.Join(dir, "io.pressure"))
	if errors.Is(err, os.ErrNotExist) {
		return model.IOPressure{State: model.IOUnreported}
	}
	if err != nil {
		return model.IOPressure{State: model.IOUnavailable}
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxIOPressureBytes+1))
	if err != nil {
		return model.IOPressure{State: model.IOUnavailable}
	}
	value, err := ParseIOPressure(data)
	if err != nil {
		return model.IOPressure{State: model.IOInvalid}
	}
	return value
}

func ParseIOPressure(data []byte) (model.IOPressure, error) {
	invalid := errors.New("invalid io.pressure sample")
	if len(data) > maxIOPressureBytes {
		return model.IOPressure{}, invalid
	}
	// Reuse the existing PSI grammar, but reject ambiguous/unknown fields before
	// parsing. Driver text and filesystem paths never enter this error boundary.
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if len(fields) != 5 {
			return model.IOPressure{}, invalid
		}
		seen := map[string]bool{}
		for _, field := range fields[1:] {
			key, _, found := strings.Cut(field, "=")
			if !found || seen[key] {
				return model.IOPressure{}, invalid
			}
			switch key {
			case "avg10", "avg60", "avg300", "total":
				seen[key] = true
			default:
				return model.IOPressure{}, invalid
			}
		}
	}
	p, err := ParseMemoryPressure(data)
	if err != nil {
		return model.IOPressure{}, invalid
	}
	value := model.IOPressure{State: model.IOAvailable,
		Some: model.PSIWindow{Avg10: p.Some.Avg10, Avg60: p.Some.Avg60, Avg300: p.Some.Avg300, TotalMicros: p.Some.TotalMicros},
		Full: model.PSIWindow{Avg10: p.Full.Avg10, Avg60: p.Full.Avg60, Avg300: p.Full.Avg300, TotalMicros: p.Full.TotalMicros}}
	if value.Validate() != nil {
		return model.IOPressure{}, invalid
	}
	return value, nil
}
