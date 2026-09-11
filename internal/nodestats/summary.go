package nodestats

import (
	"context"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/nodecontext"
)

type summary struct {
	nodeName string
	stats    nodecontext.Stats
	partial  bool
}

func decodeSummary(ctx context.Context, data []byte, nodeName string, now time.Time) (summary, error) {
	if len(data) > nodecontext.MaxSummaryBytes {
		return summary{}, errJSON
	}
	value := summary{stats: nodecontext.Stats{Provenance: nodecontext.Unknown}}
	d := newDecoder(ctx, data)
	err := d.object(map[string]func() error{
		"node": func() error { return d.nodeSummary(&value) },
	})
	if err != nil || d.finish() != nil || value.nodeName != nodeName || value.stats.StartedAt.IsZero() || value.stats.StartedAt.After(now.Add(30*time.Second)) {
		return summary{}, errJSON
	}
	if !validSampleTimes(value.stats, now) {
		return summary{}, errJSON
	}
	value.partial = value.partial || !completeMemory(value.stats.Memory) || !completeSwap(value.stats.Swap) || len(value.stats.SystemContainers) != nodecontext.MaxSystemContainers
	return value, nil
}

func (d *decoder) nodeSummary(value *summary) error {
	return d.object(map[string]func() error{
		"nodeName":  func() error { return d.stringInto(&value.nodeName, nodecontext.MaxNodeNameBytes) },
		"startTime": func() error { return d.timeInto(&value.stats.StartedAt) },
		"memory": func() error {
			memory, err := d.memory()
			value.stats.Memory = memory
			return err
		},
		"swap": func() error {
			swap, err := d.swap()
			value.stats.Swap = swap
			return err
		},
		"systemContainers": func() error { return d.systemContainers(value) },
	})
}

func (d *decoder) systemContainers(value *summary) error {
	seen := map[nodecontext.SystemCategory]bool{}
	return d.array(func(int) error {
		var name string
		item := nodecontext.SystemContainer{}
		err := d.object(map[string]func() error{
			"name":      func() error { return d.stringInto(&name, 253) },
			"startTime": func() error { return d.timeInto(&item.StartedAt) },
			"memory": func() error {
				memory, err := d.memory()
				item.Memory = memory
				return err
			},
			"swap": func() error {
				swap, err := d.swap()
				item.Swap = swap
				return err
			},
		})
		if err != nil || name == "" {
			return errJSON
		}
		switch nodecontext.SystemCategory(name) {
		case nodecontext.Kubelet, nodecontext.Runtime, nodecontext.Misc, nodecontext.Pods:
			item.Category = nodecontext.SystemCategory(name)
		default:
			value.partial = true
			return nil
		}
		if seen[item.Category] {
			return errJSON
		}
		seen[item.Category] = true
		value.partial = value.partial || !completeMemory(item.Memory) || item.StartedAt.IsZero()
		value.stats.SystemContainers = append(value.stats.SystemContainers, item)
		return nil
	})
}

func completeMemory(value *nodecontext.Memory) bool {
	return value != nil && value.UsageBytes != nil && value.AvailableBytes != nil && value.WorkingSetBytes != nil &&
		value.RSSBytes != nil && value.PageFaults != nil && value.MajorPageFaults != nil && value.PSI != nil
}

func completeSwap(value *nodecontext.Swap) bool {
	return value != nil && value.UsageBytes != nil && value.AvailableBytes != nil
}

func hasMeasurements(stats nodecontext.Stats) bool {
	known := func(memory *nodecontext.Memory, swap *nodecontext.Swap) bool {
		return (memory != nil && (memory.UsageBytes != nil || memory.AvailableBytes != nil || memory.WorkingSetBytes != nil || memory.RSSBytes != nil || memory.PageFaults != nil || memory.MajorPageFaults != nil || memory.PSI != nil)) ||
			(swap != nil && (swap.UsageBytes != nil || swap.AvailableBytes != nil))
	}
	if known(stats.Memory, stats.Swap) {
		return true
	}
	for _, item := range stats.SystemContainers {
		if known(item.Memory, item.Swap) {
			return true
		}
	}
	return false
}

func sampleTimes(stats nodecontext.Stats) []time.Time {
	result := make([]time.Time, 0, 2+2*nodecontext.MaxSystemContainers)
	add := func(memory *nodecontext.Memory, swap *nodecontext.Swap) {
		if memory != nil {
			result = append(result, memory.CapturedAt)
		}
		if swap != nil {
			result = append(result, swap.CapturedAt)
		}
	}
	add(stats.Memory, stats.Swap)
	for _, item := range stats.SystemContainers {
		add(item.Memory, item.Swap)
	}
	return result
}

func validSampleTimes(stats nodecontext.Stats, now time.Time) bool {
	for _, at := range sampleTimes(stats) {
		if at.IsZero() || at.After(now.Add(30*time.Second)) || at.Before(now.Add(-2*time.Minute)) || at.Before(stats.StartedAt) {
			return false
		}
	}
	for _, item := range stats.SystemContainers {
		if item.StartedAt.After(now.Add(30 * time.Second)) {
			return false
		}
		if item.Memory != nil && item.Memory.CapturedAt.Before(item.StartedAt) {
			return false
		}
		if item.Swap != nil && item.Swap.CapturedAt.Before(item.StartedAt) {
			return false
		}
	}
	return true
}
