package main

import (
	"strconv"
	"strings"
)

func cpuList(text string) ([]uint32, error) {
	if len(text) == 0 || len(text) > 512 {
		return nil, errInput
	}
	cpus := []uint32{}
	for _, part := range strings.Split(text, ",") {
		bounds := strings.Split(part, "-")
		if len(bounds) > 2 {
			return nil, errInput
		}
		first, err := strconv.ParseUint(bounds[0], 10, 12)
		if err != nil {
			return nil, errInput
		}
		last := first
		if len(bounds) == 2 {
			last, err = strconv.ParseUint(bounds[1], 10, 12)
			if err != nil || last <= first {
				return nil, errInput
			}
		}
		if last-first+1 > 64-uint64(len(cpus)) {
			return nil, errInput
		}
		for n := first; n <= last; n++ {
			if len(cpus) > 0 && n <= uint64(cpus[len(cpus)-1]) {
				return nil, errInput
			}
			cpus = append(cpus, uint32(n))
		}
	}
	return cpus, nil
}
