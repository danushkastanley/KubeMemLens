package main

import (
	"io"
	"os"
	"slices"
	"strconv"
	"strings"
)

func boundedKernelRead(path string, maximum int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, errConfiguration
	}
	raw, readErr := io.ReadAll(io.LimitReader(f, maximum+1))
	closeErr := f.Close()
	if readErr != nil || closeErr != nil || int64(len(raw)) > maximum {
		return nil, errConfiguration
	}
	return raw, nil
}

func onlineCPUs(raw []byte) ([]uint32, error) {
	if len(raw) == 0 || len(raw) > 512 {
		return nil, errConfiguration
	}
	var result []uint32
	for _, segment := range strings.Split(strings.TrimSpace(string(raw)), ",") {
		bounds := strings.Split(segment, "-")
		if len(bounds) < 1 || len(bounds) > 2 {
			return nil, errConfiguration
		}
		first, err := strconv.ParseUint(bounds[0], 10, 10)
		if err != nil {
			return nil, errConfiguration
		}
		last := first
		if len(bounds) == 2 {
			last, err = strconv.ParseUint(bounds[1], 10, 10)
			if err != nil || last <= first {
				return nil, errConfiguration
			}
		}
		if last-first+1 > uint64(64-len(result)) || (len(result) > 0 && uint32(first) <= result[len(result)-1]) {
			return nil, errConfiguration
		}
		for cpu := first; cpu <= last; cpu++ {
			result = append(result, uint32(cpu))
		}
	}
	if len(result) < 2 {
		return nil, errConfiguration
	}
	return result, nil
}

func sameKernel(c config) error {
	raw, err := boundedKernelRead("/proc/sys/kernel/random/boot_id", 64)
	if err != nil || strings.TrimSpace(string(raw)) != c.Boot {
		return errConfiguration
	}
	raw, err = boundedKernelRead("/sys/devices/system/cpu/online", 512)
	if err != nil {
		return err
	}
	cpus, err := onlineCPUs(raw)
	if err != nil || !slices.Equal(cpus, c.CPUs) {
		return errConfiguration
	}
	return nil
}
