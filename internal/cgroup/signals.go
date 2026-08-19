package cgroup

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/danushkastanley/kube-memlens/internal/model"
)

type pressureSample struct {
	Avg10       float64
	Avg60       float64
	Avg300      float64
	TotalMicros uint64
}

type memoryPressure struct {
	Some pressureSample
	Full pressureSample
}

func readMemorySignals(dir string, breakdown *model.MemoryBreakdown) error {
	var err error
	if breakdown.PeakBytes, breakdown.PeakKnown, _, err = readOptionalScalar(dir, "memory.peak", false); err != nil {
		return err
	}
	if breakdown.MinBytes, breakdown.MinKnown, breakdown.MinUnlimited, err = readOptionalScalar(dir, "memory.min", true); err != nil {
		return err
	}
	if breakdown.LowBytes, breakdown.LowKnown, breakdown.LowUnlimited, err = readOptionalScalar(dir, "memory.low", true); err != nil {
		return err
	}
	if breakdown.HighBytes, breakdown.HighKnown, breakdown.HighUnlimited, err = readOptionalScalar(dir, "memory.high", true); err != nil {
		return err
	}
	if breakdown.MaxBytes, breakdown.MaxKnown, breakdown.MaxUnlimited, err = readOptionalScalar(dir, "memory.max", true); err != nil {
		return err
	}
	if breakdown.SwapCurrentBytes, breakdown.SwapCurrentKnown, _, err = readOptionalScalar(dir, "memory.swap.current", false); err != nil {
		return err
	}
	if breakdown.SwapPeakBytes, breakdown.SwapPeakKnown, _, err = readOptionalScalar(dir, "memory.swap.peak", false); err != nil {
		return err
	}
	if breakdown.SwapMaxBytes, breakdown.SwapMaxKnown, breakdown.SwapMaxUnlimited, err = readOptionalScalar(dir, "memory.swap.max", true); err != nil {
		return err
	}

	local, known, err := readOptionalEvents(dir, "memory.events.local", knownEventKeys)
	if err != nil {
		return err
	}
	breakdown.LocalEventsKnown = known
	breakdown.LocalOOMEvents = local["oom"]
	breakdown.LocalOOMKillEvents = local["oom_kill"]
	breakdown.LocalHighEvents = local["high"]
	breakdown.LocalMaxEvents = local["max"]

	swap, known, err := readOptionalEvents(dir, "memory.swap.events", knownSwapEventKeys)
	if err != nil {
		return err
	}
	breakdown.SwapEventsKnown = known
	breakdown.SwapHighEvents = swap["high"]
	breakdown.SwapMaxEvents = swap["max"]
	breakdown.SwapFailEvents = swap["fail"]

	pressureBytes, err := os.ReadFile(filepath.Join(dir, "memory.pressure"))
	if err == nil {
		pressure, parseErr := ParseMemoryPressure(pressureBytes)
		if parseErr != nil {
			return fmt.Errorf("parse memory.pressure: %w", parseErr)
		}
		breakdown.PressureKnown = true
		breakdown.PSISomeAvg10 = pressure.Some.Avg10
		breakdown.PSISomeAvg60 = pressure.Some.Avg60
		breakdown.PSISomeAvg300 = pressure.Some.Avg300
		breakdown.PSISomeTotalMicros = pressure.Some.TotalMicros
		breakdown.PSIFullAvg10 = pressure.Full.Avg10
		breakdown.PSIFullAvg60 = pressure.Full.Avg60
		breakdown.PSIFullAvg300 = pressure.Full.Avg300
		breakdown.PSIFullTotalMicros = pressure.Full.TotalMicros
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read memory.pressure: %w", err)
	}
	return nil
}

func readOptionalScalar(dir, name string, allowMax bool) (value uint64, known bool, unlimited bool, err error) {
	data, err := os.ReadFile(filepath.Join(dir, name))
	if errors.Is(err, os.ErrNotExist) {
		return 0, false, false, nil
	}
	if err != nil {
		return 0, false, false, fmt.Errorf("read %s: %w", name, err)
	}
	raw := strings.TrimSpace(string(data))
	if allowMax && raw == "max" {
		return 0, true, true, nil
	}
	value, err = ParseMemoryCurrent(data)
	if err != nil {
		return 0, false, false, fmt.Errorf("parse %s: %w", name, err)
	}
	return value, true, false, nil
}

func readOptionalEvents(dir, name string, keys map[string]struct{}) (map[string]uint64, bool, error) {
	data, err := os.ReadFile(filepath.Join(dir, name))
	if errors.Is(err, os.ErrNotExist) {
		return map[string]uint64{}, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("read %s: %w", name, err)
	}
	values, _, err := parseKeyValues(data, keys)
	if err != nil {
		return nil, false, fmt.Errorf("parse %s: %w", name, err)
	}
	return values, true, nil
}

func ParseMemoryPressure(data []byte) (memoryPressure, error) {
	var pressure memoryPressure
	seen := map[string]bool{}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for lineNo := 1; scanner.Scan(); lineNo++ {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 0 {
			continue
		}
		if fields[0] != "some" && fields[0] != "full" {
			return memoryPressure{}, fmt.Errorf("line %d: unknown pressure class %q", lineNo, fields[0])
		}
		if seen[fields[0]] {
			return memoryPressure{}, fmt.Errorf("line %d: duplicate pressure class %q", lineNo, fields[0])
		}
		seen[fields[0]] = true
		sample, err := parsePressureSample(fields[1:])
		if err != nil {
			return memoryPressure{}, fmt.Errorf("line %d: %w", lineNo, err)
		}
		if fields[0] == "some" {
			pressure.Some = sample
		} else {
			pressure.Full = sample
		}
	}
	if err := scanner.Err(); err != nil {
		return memoryPressure{}, err
	}
	if !seen["some"] || !seen["full"] {
		return memoryPressure{}, errors.New("expected some and full pressure lines")
	}
	return pressure, nil
}

func parsePressureSample(fields []string) (pressureSample, error) {
	values := map[string]string{}
	for _, field := range fields {
		parts := strings.SplitN(field, "=", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return pressureSample{}, fmt.Errorf("invalid pressure field %q", field)
		}
		values[parts[0]] = parts[1]
	}
	for _, key := range []string{"avg10", "avg60", "avg300", "total"} {
		if values[key] == "" {
			return pressureSample{}, fmt.Errorf("missing %s", key)
		}
	}
	avg10, err := strconv.ParseFloat(values["avg10"], 64)
	if err != nil {
		return pressureSample{}, fmt.Errorf("invalid avg10: %w", err)
	}
	avg60, err := strconv.ParseFloat(values["avg60"], 64)
	if err != nil {
		return pressureSample{}, fmt.Errorf("invalid avg60: %w", err)
	}
	avg300, err := strconv.ParseFloat(values["avg300"], 64)
	if err != nil {
		return pressureSample{}, fmt.Errorf("invalid avg300: %w", err)
	}
	total, err := strconv.ParseUint(values["total"], 10, 64)
	if err != nil {
		return pressureSample{}, fmt.Errorf("invalid total: %w", err)
	}
	return pressureSample{Avg10: avg10, Avg60: avg60, Avg300: avg300, TotalMicros: total}, nil
}

var knownSwapEventKeys = map[string]struct{}{
	"high": {},
	"max":  {},
	"fail": {},
}
