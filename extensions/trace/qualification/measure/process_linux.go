//go:build linux

package main

import (
	"errors"
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

func (g *group) resident() (uint64, int, error) {
	b, err := g.read("cgroup.procs")
	if err != nil {
		return 0, 0, err
	}
	ids, err := processIDs(b)
	if err != nil {
		return 0, 0, err
	}
	var total uint64
	for _, pid := range ids {
		before, err := procRead(pid, "stat")
		if err != nil {
			return 0, 0, err
		}
		start, err := processStart(before)
		if err != nil || start == 0 {
			return 0, 0, errors.New("invalid process lifetime")
		}
		membership, err := procRead(pid, "cgroup")
		if err != nil || string(membership) != "0::"+strings.TrimPrefix(g.spec.Path, "/sys/fs/cgroup")+"\n" {
			return 0, 0, errors.New("process membership changed")
		}
		statm, err := procRead(pid, "statm")
		if err != nil {
			return 0, 0, err
		}
		fields := strings.Fields(string(statm))
		if len(fields) != 7 {
			return 0, 0, errors.New("invalid resident observation")
		}
		pages, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil || pages > 1<<40 {
			return 0, 0, errors.New("invalid resident pages")
		}
		after, err := procRead(pid, "stat")
		if err != nil {
			return 0, 0, err
		}
		end, err := processStart(after)
		if err != nil || start != end {
			return 0, 0, errors.New("process lifetime changed")
		}
		total += pages * uint64(os.Getpagesize())
	}
	b, err = g.read("cgroup.procs")
	if err != nil {
		return 0, 0, err
	}
	end, err := processIDs(b)
	if err != nil || !slices.Equal(ids, end) {
		return 0, 0, errors.New("process set changed")
	}
	return total, len(ids), nil
}
