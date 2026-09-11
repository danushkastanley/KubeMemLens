package main

import (
	"io"
	"os"
	"runtime"
	"strings"

	"github.com/danushkastanley/kube-memlens/internal/nodecontext"
	"github.com/danushkastanley/kube-memlens/internal/nodestats"
)

// This reads only the producer's own cgroup membership, not host /proc or
// another workload. A Node object cannot establish cgroup-controller mode.
func checkLocalPlatform() error {
	if runtime.GOOS != "linux" {
		return &nodestats.Error{Reason: nodecontext.Unsupported}
	}
	file, err := os.Open("/proc/self/cgroup")
	if err != nil {
		return &nodestats.Error{Reason: nodecontext.Unsupported}
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 65537))
	if err != nil || len(data) > 65536 || !memoryUsesCgroupV2(string(data)) {
		return &nodestats.Error{Reason: nodecontext.Unsupported}
	}
	return nil
}

func memoryUsesCgroupV2(data string) bool {
	unified := false
	for _, line := range strings.Split(strings.TrimSpace(data), "\n") {
		fields := strings.SplitN(line, ":", 3)
		if len(fields) != 3 {
			return false
		}
		if fields[0] == "0" && fields[1] == "" {
			unified = true
		}
		for _, controller := range strings.Split(fields[1], ",") {
			if controller == "memory" {
				return false
			}
		}
	}
	return unified
}
