//go:build linux

package main

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"math/bits"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type processSample struct {
	State      string           `json:"state"`
	RSSBytes   *uint64          `json:"rssBytes,omitempty"`
	Count      *int             `json:"count,omitempty"`
	Tasks      *int             `json:"tasks,omitempty"`
	Scheduling *schedulerSample `json:"scheduling,omitempty"`
}

type schedulerSample struct {
	State             string  `json:"state"`
	Source            string  `json:"source"`
	Enabled           *bool   `json:"enabled,omitempty"`
	Version           int     `json:"version,omitempty"`
	CPUs              int     `json:"cpus,omitempty"`
	Cohort            string  `json:"cohort,omitempty"`
	RuntimeNanos      *uint64 `json:"runtimeNanos,omitempty"`
	RunqueueWaitNanos *uint64 `json:"runqueueWaitNanos,omitempty"`
	Timeslices        *uint64 `json:"timeslices,omitempty"`
}

type totals struct{ run, wait, slices uint64 }

func (s *schedulerSample) setTotals(v totals) {
	s.RuntimeNanos = &v.run
	s.RunqueueWaitNanos = &v.wait
	s.Timeslices = &v.slices
}
func (s schedulerSample) Totals() totals {
	return totals{*s.RuntimeNanos, *s.RunqueueWaitNanos, *s.Timeslices}
}
func addTotals(a *totals, b totals) error {
	var carry uint64
	for _, pair := range []struct {
		dst   *uint64
		value uint64
	}{{&a.run, b.run}, {&a.wait, b.wait}, {&a.slices, b.slices}} {
		*pair.dst, carry = bits.Add64(*pair.dst, pair.value, 0)
		if carry != 0 {
			return errors.New("scheduling counter overflow")
		}
	}
	return nil
}
func parseTaskScheduling(data []byte) (totals, error) {
	fields := strings.Fields(string(data))
	var result totals
	if len(fields) != 3 {
		return result, errors.New("invalid task scheduling")
	}
	for i, p := range []*uint64{&result.run, &result.wait, &result.slices} {
		n, err := strconv.ParseUint(fields[i], 10, 64)
		if err != nil {
			return totals{}, err
		}
		*p = n
	}
	return result, nil
}
func processScheduling(pid int) (schedulerSample, int, error) {
	result := schedulerSample{State: "observed", Source: "/proc/pid/task/tid/schedstat"}
	dir := filepath.Join("/proc", strconv.Itoa(pid), "task")
	f, err := os.Open(dir)
	if err != nil {
		return result, 0, err
	}
	defer f.Close()
	entries, err := f.ReadDir(257)
	if err != nil || len(entries) == 0 || len(entries) > 256 {
		return result, 0, errors.New("invalid task set")
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	ids := make([]string, 0, len(entries))
	hash := sha256.New()
	var sum totals
	for _, entry := range entries {
		tid, err := strconv.Atoi(entry.Name())
		if err != nil || tid <= 1 || !entry.IsDir() {
			return result, 0, errors.New("invalid task")
		}
		path := filepath.Join(dir, entry.Name())
		first, err := readFile(path + "/stat")
		if err != nil {
			return result, 0, err
		}
		started, err := processStart(first)
		if err != nil || started == 0 {
			return result, 0, errors.New("invalid task lifetime")
		}
		raw, err := readFile(path + "/schedstat")
		if err != nil {
			return result, 0, err
		}
		values, err := parseTaskScheduling(raw)
		if err != nil {
			return result, 0, err
		}
		last, err := readFile(path + "/stat")
		if err != nil {
			return result, 0, err
		}
		ended, err := processStart(last)
		if err != nil || ended != started {
			return result, 0, errors.New("task lifetime changed")
		}
		if addTotals(&sum, values) != nil {
			return result, 0, errors.New("scheduling counter overflow")
		}
		fmt.Fprintf(hash, "%d:%d;", tid, started)
		ids = append(ids, entry.Name())
	}
	// Detect additions as well as exits; totals from changing cohorts cannot form deltas.
	end, err := os.Open(dir)
	if err != nil {
		return result, 0, err
	}
	defer end.Close()
	after, err := end.ReadDir(257)
	if err != nil || len(after) != len(entries) {
		return result, 0, errors.New("task set changed")
	}
	names := make([]string, 0, len(after))
	for _, e := range after {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	if strings.Join(ids, ",") != strings.Join(names, ",") {
		return result, 0, errors.New("task set changed")
	}
	result.setTotals(sum)
	result.Cohort = fmt.Sprintf("%x", hash.Sum(nil))
	return result, len(entries), nil
}
func nodeScheduling(path, setting string) schedulerSample {
	raw, err := readFile(path)
	if os.IsNotExist(err) {
		return schedulerSample{State: "unsupported", Source: "/proc/schedstat"}
	}
	if err != nil {
		return schedulerSample{State: "unavailable", Source: "/proc/schedstat"}
	}
	flag, err := readFile(setting)
	if err != nil {
		return schedulerSample{State: "unavailable", Source: "/proc/schedstat"}
	}
	enabled := strings.TrimSpace(string(flag))
	if enabled == "0" {
		value := false
		return schedulerSample{State: "disabled", Source: "/proc/schedstat", Enabled: &value}
	}
	if enabled != "1" {
		return schedulerSample{State: "unavailable", Source: "/proc/schedstat"}
	}
	result, err := parseNodeScheduling(raw)
	if err != nil {
		return schedulerSample{State: "unavailable", Source: "/proc/schedstat"}
	}
	value := true
	result.Enabled = &value
	return result
}
func parseNodeScheduling(raw []byte) (schedulerSample, error) {
	s := schedulerSample{State: "observed", Source: "/proc/schedstat"}
	var sum totals
	seen := map[string]bool{}
	timestamp := false
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			return s, errors.New("empty scheduler line")
		}
		switch {
		case fields[0] == "version":
			if len(fields) != 2 || s.Version != 0 {
				return s, errors.New("invalid version")
			}
			version, err := strconv.Atoi(fields[1])
			if err != nil || version < 15 || version > 17 {
				return s, errors.New("unsupported version")
			}
			s.Version = version
		case fields[0] == "timestamp":
			if len(fields) != 2 || timestamp {
				return s, errors.New("invalid scheduler clock")
			}
			if _, err := strconv.ParseUint(fields[1], 10, 64); err != nil {
				return s, err
			}
			timestamp = true
		case strings.HasPrefix(fields[0], "cpu"):
			if len(fields) != 10 || seen[fields[0]] || len(seen) >= 4096 {
				return s, errors.New("invalid CPU scheduling")
			}
			if _, err := strconv.ParseUint(strings.TrimPrefix(fields[0], "cpu"), 10, 32); err != nil {
				return s, err
			}
			for _, v := range fields[1:] {
				if _, err := strconv.ParseUint(v, 10, 64); err != nil {
					return s, err
				}
			}
			t, err := parseTaskScheduling([]byte(strings.Join(fields[7:10], " ")))
			if err != nil {
				return s, err
			}
			if err := addTotals(&sum, t); err != nil {
				return s, err
			}
			seen[fields[0]] = true
		case strings.HasPrefix(fields[0], "domain"):
			// Domain counters are not part of this source's CPU runqueue total.
		default:
			return s, errors.New("unknown scheduler line")
		}
	}
	if s.Version == 0 || !timestamp || len(seen) == 0 {
		return s, errors.New("missing scheduling fields")
	}
	s.CPUs = len(seen)
	s.setTotals(sum)
	return s, nil
}
