//go:build linux

package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

func processIDs(data []byte) ([]int, error) {
	ids := []int{}
	for _, field := range strings.Fields(string(data)) {
		id, err := strconv.Atoi(field)
		if err != nil || id <= 1 || len(ids) >= 64 {
			return nil, errors.New("invalid process set")
		}
		ids = append(ids, id)
	}
	slices.Sort(ids)
	ids = slices.Compact(ids)
	if len(ids) == 0 {
		return nil, errors.New("empty process set")
	}
	return ids, nil
}

func procRead(pid int, name string) ([]byte, error) {
	f, err := os.Open(filepath.Join("/proc", strconv.Itoa(pid), name))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return readBounded(f)
}

func processStart(data []byte) (uint64, error) {
	end := strings.LastIndex(string(data), ")")
	if end < 0 {
		return 0, errors.New("invalid process stat")
	}
	fields := strings.Fields(string(data[end+1:]))
	if len(fields) < 20 {
		return 0, errors.New("short process stat")
	}
	return strconv.ParseUint(fields[19], 10, 64)
}

func (g *group) processes() (processSample, error) {
	value, err := g.stableProcesses()
	if err != nil {
		return processSample{State: "unavailable"}, nil
	}
	return value, nil
}

func (g *group) stableProcesses() (processSample, error) {
	b, err := g.read("cgroup.procs")
	if err != nil {
		return processSample{}, err
	}
	ids, err := processIDs(b)
	if err != nil {
		return processSample{}, err
	}
	var total uint64
	cohort := hmac.New(sha256.New, g.cohortKey[:])
	wait := schedulerSample{State: "observed", Source: "/proc/pid/task/tid/schedstat"}
	var scheduling totals
	tasks := 0
	for _, pid := range ids {
		before, err := procRead(pid, "stat")
		if err != nil {
			return processSample{}, err
		}
		start, err := processStart(before)
		if err != nil || start == 0 {
			return processSample{}, errors.New("invalid process lifetime")
		}
		membership, err := procRead(pid, "cgroup")
		if err != nil || string(membership) != "0::"+strings.TrimPrefix(g.spec.Path, "/sys/fs/cgroup")+"\n" {
			return processSample{}, errors.New("process membership changed")
		}
		fmt.Fprintf(cohort, "%d:%d;", pid, start)
		taskStats, taskCount, err := processScheduling(pid)
		if err != nil {
			wait.State = "unavailable"
		} else if addTotals(&scheduling, taskStats.Totals()) != nil {
			wait.State = "unavailable"
		}
		fmt.Fprint(cohort, taskStats.Cohort)
		tasks += taskCount
		statm, err := procRead(pid, "statm")
		if err != nil {
			return processSample{}, err
		}
		fields := strings.Fields(string(statm))
		if len(fields) != 7 {
			return processSample{}, errors.New("invalid resident observation")
		}
		pages, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil || pages > 1<<40 {
			return processSample{}, errors.New("invalid resident pages")
		}
		after, err := procRead(pid, "stat")
		if err != nil {
			return processSample{}, err
		}
		end, err := processStart(after)
		if err != nil || start != end {
			return processSample{}, errors.New("process lifetime changed")
		}
		total += pages * uint64(os.Getpagesize())
	}
	b, err = g.read("cgroup.procs")
	if err != nil {
		return processSample{}, err
	}
	end, err := processIDs(b)
	if err != nil || !slices.Equal(ids, end) {
		return processSample{}, errors.New("process set changed")
	}
	if wait.State == "observed" {
		wait.setTotals(scheduling)
		wait.Cohort = fmt.Sprintf("%x", cohort.Sum(nil))
	}
	count := len(ids)
	value := processSample{State: "observed", RSSBytes: &total, Count: &count, Scheduling: &wait}
	if wait.State == "observed" {
		value.Tasks = &tasks
	}
	return value, nil
}
