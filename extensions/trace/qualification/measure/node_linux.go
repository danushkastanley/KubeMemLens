//go:build linux

package main

import (
	"errors"
	"os"
	"strconv"
	"strings"
	"syscall"
)

type nodeSample struct {
	CPUTicks             []uint64                     `json:"cpuTicks"`
	MemoryAvailable      uint64                       `json:"memoryAvailable"`
	Pressure             map[string]map[string]uint64 `json:"pressure"`
	ObserverCPUUsec      int64                        `json:"observerCPUUsec"`
	ObserverPeakRSSBytes int64                        `json:"observerPeakRSSBytes"`
}

func readFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return readBounded(f)
}

func pressure(data []byte) (map[string]uint64, error) {
	out := map[string]uint64{}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 5 || (fields[0] != "some" && fields[0] != "full") || !strings.HasPrefix(fields[4], "total=") {
			return nil, errors.New("invalid pressure")
		}
		if _, exists := out[fields[0]]; exists {
			return nil, errors.New("duplicate pressure")
		}
		v, err := strconv.ParseUint(strings.TrimPrefix(fields[4], "total="), 10, 64)
		if err != nil {
			return nil, err
		}
		out[fields[0]] = v
	}
	if _, ok := out["some"]; !ok {
		return nil, errors.New("missing pressure")
	}
	return out, nil
}

func readNode() (nodeSample, error) {
	n := nodeSample{Pressure: map[string]map[string]uint64{}}
	b, err := readFile("/proc/stat")
	if err != nil {
		return n, err
	}
	fields := strings.Fields(strings.SplitN(string(b), "\n", 2)[0])
	if len(fields) != 11 || fields[0] != "cpu" {
		return n, errors.New("invalid CPU counters")
	}
	for _, field := range fields[1:] {
		v, err := strconv.ParseUint(field, 10, 64)
		if err != nil {
			return n, err
		}
		n.CPUTicks = append(n.CPUTicks, v)
	}
	b, err = readFile("/proc/meminfo")
	if err != nil {
		return n, err
	}
	found := false
	for _, line := range strings.Split(string(b), "\n") {
		if !strings.HasPrefix(line, "MemAvailable:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 3 || fields[2] != "kB" || found {
			return n, errors.New("invalid node memory")
		}
		v, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil || v > 1<<50 {
			return n, errors.New("invalid node memory")
		}
		n.MemoryAvailable = v * 1024
		found = true
	}
	if !found {
		return n, errors.New("missing node memory")
	}
	for _, kind := range []string{"cpu", "memory", "io"} {
		b, err := readFile("/proc/pressure/" + kind)
		if err != nil {
			return n, err
		}
		n.Pressure[kind], err = pressure(b)
		if err != nil {
			return n, err
		}
	}
	var usage syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &usage); err != nil {
		return n, err
	}
	n.ObserverCPUUsec = usage.Utime.Sec*1e6 + usage.Utime.Usec + usage.Stime.Sec*1e6 + usage.Stime.Usec
	n.ObserverPeakRSSBytes = usage.Maxrss * 1024
	return n, nil
}
