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

type MemoryStat struct {
	Values  map[string]uint64
	Unknown map[string]uint64
}

type MemoryEvents struct {
	Values  map[string]uint64
	Unknown map[string]uint64
}

func ParseDirectory(name, dir string) (model.MemoryBreakdown, error) {
	currentBytes, err := os.ReadFile(filepath.Join(dir, "memory.current"))
	if err != nil {
		return model.MemoryBreakdown{}, fmt.Errorf("read memory.current: %w", err)
	}

	total, err := ParseMemoryCurrent(currentBytes)
	if err != nil {
		return model.MemoryBreakdown{}, fmt.Errorf("parse memory.current: %w", err)
	}

	statBytes, err := os.ReadFile(filepath.Join(dir, "memory.stat"))
	if err != nil {
		return model.MemoryBreakdown{}, fmt.Errorf("read memory.stat: %w", err)
	}

	stat, err := ParseMemoryStat(statBytes)
	if err != nil {
		return model.MemoryBreakdown{}, fmt.Errorf("parse memory.stat: %w", err)
	}

	events := MemoryEvents{Values: map[string]uint64{}, Unknown: map[string]uint64{}}
	eventBytes, err := os.ReadFile(filepath.Join(dir, "memory.events"))
	if err == nil {
		events, err = ParseMemoryEvents(eventBytes)
		if err != nil {
			return model.MemoryBreakdown{}, fmt.Errorf("parse memory.events: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return model.MemoryBreakdown{}, fmt.Errorf("read memory.events: %w", err)
	}

	slab := stat.value("slab")
	if slab == 0 {
		slab = stat.value("slab_reclaimable") + stat.value("slab_unreclaimable")
	}

	return model.MemoryBreakdown{
		Name:              name,
		TotalBytes:        total,
		AnonBytes:         stat.value("anon"),
		FileBytes:         stat.value("file"),
		ActiveFileBytes:   stat.value("active_file"),
		InactiveFileBytes: stat.value("inactive_file"),
		ShmemBytes:        stat.value("shmem"),
		SlabBytes:         slab,
		KernelBytes:       stat.value("kernel"),
		DirtyBytes:        stat.value("file_dirty"),
		WritebackBytes:    stat.value("file_writeback"),
		OOMEvents:         events.value("oom"),
		OOMKillEvents:     events.value("oom_kill"),
		HighEvents:        events.value("high"),
		MaxEvents:         events.value("max"),
	}, nil
}

func ParseMemoryCurrent(data []byte) (uint64, error) {
	value := strings.TrimSpace(string(data))
	if value == "" {
		return 0, errors.New("empty memory.current")
	}

	parsed, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid byte value %q: %w", value, err)
	}

	return parsed, nil
}

func ParseMemoryStat(data []byte) (MemoryStat, error) {
	values, unknown, err := parseKeyValues(data, knownStatKeys)
	if err != nil {
		return MemoryStat{}, err
	}

	return MemoryStat{Values: values, Unknown: unknown}, nil
}

func ParseMemoryEvents(data []byte) (MemoryEvents, error) {
	values, unknown, err := parseKeyValues(data, knownEventKeys)
	if err != nil {
		return MemoryEvents{}, err
	}

	return MemoryEvents{Values: values, Unknown: unknown}, nil
}

func parseKeyValues(data []byte, known map[string]struct{}) (map[string]uint64, map[string]uint64, error) {
	values := make(map[string]uint64)
	unknown := make(map[string]uint64)
	scanner := bufio.NewScanner(bytes.NewReader(data))

	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) != 2 {
			return nil, nil, fmt.Errorf("line %d: expected key/value pair", lineNo)
		}

		value, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			return nil, nil, fmt.Errorf("line %d: invalid value for %s: %w", lineNo, fields[0], err)
		}

		if _, ok := known[fields[0]]; ok {
			values[fields[0]] = value
			continue
		}
		unknown[fields[0]] = value
	}

	if err := scanner.Err(); err != nil {
		return nil, nil, err
	}

	return values, unknown, nil
}

func (s MemoryStat) value(key string) uint64 {
	return s.Values[key]
}

func (e MemoryEvents) value(key string) uint64 {
	return e.Values[key]
}

var knownStatKeys = map[string]struct{}{
	"anon":                     {},
	"file":                     {},
	"kernel":                   {},
	"kernel_stack":             {},
	"pagetables":               {},
	"sec_pagetables":           {},
	"percpu":                   {},
	"sock":                     {},
	"vmalloc":                  {},
	"shmem":                    {},
	"zswap":                    {},
	"zswapped":                 {},
	"file_mapped":              {},
	"file_dirty":               {},
	"file_writeback":           {},
	"swapcached":               {},
	"anon_thp":                 {},
	"file_thp":                 {},
	"shmem_thp":                {},
	"inactive_anon":            {},
	"active_anon":              {},
	"inactive_file":            {},
	"active_file":              {},
	"unevictable":              {},
	"slab_reclaimable":         {},
	"slab_unreclaimable":       {},
	"slab":                     {},
	"workingset_refault_anon":  {},
	"workingset_refault_file":  {},
	"workingset_activate_anon": {},
	"workingset_activate_file": {},
	"workingset_restore_anon":  {},
	"workingset_restore_file":  {},
	"workingset_nodereclaim":   {},
	"pgfault":                  {},
	"pgmajfault":               {},
	"pgrefill":                 {},
	"pgscan":                   {},
	"pgsteal":                  {},
	"pgactivate":               {},
	"pgdeactivate":             {},
	"pglazyfree":               {},
	"pglazyfreed":              {},
	"thp_fault_alloc":          {},
	"thp_collapse_alloc":       {},
}

var knownEventKeys = map[string]struct{}{
	"low":            {},
	"high":           {},
	"max":            {},
	"oom":            {},
	"oom_kill":       {},
	"oom_group_kill": {},
}
