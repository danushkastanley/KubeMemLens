// Package agentless reads only the Kubernetes APIs permitted by the caller.
package agentless

import (
	"fmt"
	"time"
)

type Options struct {
	Timeout          time.Duration
	MaxAge           time.Duration
	MaxResponseBytes int64
	MaxTotalBytes    int64
	MaxOutputBytes   int64
	PageSize         int
	MaxPages         int
	MaxPods          int
	MaxContainers    int
	MaxNodes         int
	MaxOwnerReads    int
	MaxRequests      int
	Now              func() time.Time
}

func normaliseOptions(opts Options) (Options, error) {
	for _, limit := range []struct {
		name    string
		value   int64
		ceiling int64
	}{
		{"response bytes", opts.MaxResponseBytes, 16 << 20}, {"refresh bytes", opts.MaxTotalBytes, 64 << 20},
		{"output bytes", opts.MaxOutputBytes, 64 << 20},
		{"page size", int64(opts.PageSize), 1000}, {"pages", int64(opts.MaxPages), 100},
		{"Pods", int64(opts.MaxPods), 10000}, {"containers", int64(opts.MaxContainers), 100000},
		{"Nodes", int64(opts.MaxNodes), 10000}, {"owner reads", int64(opts.MaxOwnerReads), 2000}, {"requests", int64(opts.MaxRequests), 5000},
	} {
		if limit.value < 0 || limit.value > limit.ceiling {
			return Options{}, fmt.Errorf("agentless %s limit is out of range", limit.name)
		}
	}
	if opts.Timeout < 0 || opts.Timeout > time.Minute || opts.MaxAge < 0 || opts.MaxAge > 24*time.Hour {
		return Options{}, fmt.Errorf("agentless time bound is out of range")
	}
	if opts.Timeout == 0 {
		opts.Timeout = 5 * time.Second
	}
	if opts.MaxAge == 0 {
		opts.MaxAge = 2 * time.Minute
	}
	if opts.MaxResponseBytes == 0 {
		opts.MaxResponseBytes = 4 << 20
	}
	if opts.MaxTotalBytes == 0 {
		opts.MaxTotalBytes = 32 << 20
	}
	if opts.MaxOutputBytes == 0 {
		opts.MaxOutputBytes = 16 << 20
	}
	if opts.PageSize == 0 {
		opts.PageSize = 500
	}
	if opts.MaxPages == 0 {
		opts.MaxPages = 4
	}
	if opts.MaxPods == 0 {
		opts.MaxPods = 2000
	}
	if opts.MaxContainers == 0 {
		opts.MaxContainers = 10000
	}
	if opts.MaxNodes == 0 {
		opts.MaxNodes = 500
	}
	if opts.MaxOwnerReads == 0 {
		opts.MaxOwnerReads = 500
	}
	if opts.MaxRequests == 0 {
		opts.MaxRequests = 1100
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return opts, nil
}
