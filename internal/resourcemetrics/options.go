package resourcemetrics

import (
	"fmt"
	"time"
)

func validateOptions(opts Options) error {
	for _, limit := range []struct {
		name           string
		value, maximum int64
	}{
		{"response bytes", opts.MaxResponseBytes, 64 << 20}, {"page size", int64(opts.PageSize), 1000},
		{"pages", int64(opts.MaxPages), 100}, {"Pods", int64(opts.MaxPods), 10000}, {"containers", int64(opts.MaxContainers), 100000},
	} {
		if limit.value < 0 || limit.value > limit.maximum {
			return fmt.Errorf("resource-metrics %s limit is out of range", limit.name)
		}
	}
	if opts.Timeout < 0 || opts.Timeout > time.Minute || opts.MaxAge < 0 || opts.MaxAge > 24*time.Hour || opts.MaxFutureSkew < 0 || opts.MaxFutureSkew > time.Minute {
		return fmt.Errorf("resource-metrics time bound is out of range")
	}
	return nil
}
