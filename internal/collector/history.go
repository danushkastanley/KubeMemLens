package collector

import (
	"sort"
	"strings"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/model"
)

type HistoryOptions struct {
	Duration          time.Duration
	MaxSeries         int
	MaxPoints         int
	MaxResponseSeries int
}

func DefaultHistoryOptions() HistoryOptions {
	return HistoryOptions{Duration: 15 * time.Minute, MaxSeries: 1000, MaxPoints: 180, MaxResponseSeries: 20}
}

type historyStore struct {
	opts   HistoryOptions
	series map[string]*api.PodHistory
}

func newHistoryStore(opts HistoryOptions) *historyStore {
	defaults := DefaultHistoryOptions()
	if opts.Duration <= 0 {
		opts.Duration = defaults.Duration
	}
	if opts.MaxSeries <= 0 {
		opts.MaxSeries = defaults.MaxSeries
	}
	if opts.MaxPoints <= 0 {
		opts.MaxPoints = defaults.MaxPoints
	}
	if opts.MaxResponseSeries <= 0 {
		opts.MaxResponseSeries = defaults.MaxResponseSeries
	}
	return &historyStore{opts: opts, series: map[string]*api.PodHistory{}}
}

func (h *historyStore) record(capturedAt time.Time, containers []api.ContainerSnapshot) {
	h.prune(capturedAt)
	byPod := map[string][]api.ContainerSnapshot{}
	for _, container := range containers {
		if container.Namespace == "" || container.PodName == "" || container.PodUID == "" {
			continue
		}
		key := historyKey(container.Namespace, container.PodName, container.PodUID, container.NodeName)
		byPod[key] = append(byPod[key], container)
	}
	keys := make([]string, 0, len(byPod))
	for key := range byPod {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		containers := byPod[key]
		series := h.series[key]
		if series == nil {
			if len(h.series) >= h.opts.MaxSeries {
				continue
			}
			first := containers[0]
			series = &api.PodHistory{Namespace: first.Namespace, PodName: first.PodName, PodUID: first.PodUID, NodeName: first.NodeName}
			h.series[key] = series
		}
		memories := make([]model.MemoryBreakdown, 0, len(containers))
		for _, container := range containers {
			memories = append(memories, container.Memory)
		}
		memory := model.SumMemory(series.Namespace+"/"+series.PodName, memories)
		point := historyPoint(capturedAt, memory)
		if len(series.Points) > 0 && series.Points[len(series.Points)-1].CapturedAt.Equal(capturedAt) {
			series.Points[len(series.Points)-1] = point
		} else {
			series.Points = append(series.Points, point)
		}
		if overflow := len(series.Points) - h.opts.MaxPoints; overflow > 0 {
			series.Points = append([]api.MemoryHistoryPoint(nil), series.Points[overflow:]...)
		}
	}
}

func (h *historyStore) list(namespace, podName, nodeName string, now time.Time) []api.PodHistory {
	h.prune(now)
	items := []api.PodHistory{}
	for _, series := range h.series {
		if series.Namespace != namespace || series.PodName != podName || (nodeName != "" && series.NodeName != nodeName) {
			continue
		}
		copy := *series
		copy.Points = append([]api.MemoryHistoryPoint(nil), series.Points...)
		items = append(items, copy)
	}
	sort.Slice(items, func(i, j int) bool {
		iLast := items[i].Points[len(items[i].Points)-1].CapturedAt
		jLast := items[j].Points[len(items[j].Points)-1].CapturedAt
		if !iLast.Equal(jLast) {
			return iLast.After(jLast)
		}
		if items[i].NodeName != items[j].NodeName {
			return items[i].NodeName < items[j].NodeName
		}
		return items[i].PodUID < items[j].PodUID
	})
	if len(items) > h.opts.MaxResponseSeries {
		items = items[:h.opts.MaxResponseSeries]
	}
	return items
}

func (h *historyStore) stats() (series, points int) {
	for _, item := range h.series {
		series++
		points += len(item.Points)
	}
	return series, points
}

func (h *historyStore) prune(now time.Time) {
	cutoff := now.Add(-h.opts.Duration)
	for key, series := range h.series {
		first := 0
		for first < len(series.Points) && series.Points[first].CapturedAt.Before(cutoff) {
			first++
		}
		if first == len(series.Points) {
			delete(h.series, key)
			continue
		}
		if first > 0 {
			series.Points = append([]api.MemoryHistoryPoint(nil), series.Points[first:]...)
		}
	}
}

func historyPoint(capturedAt time.Time, memory model.MemoryBreakdown) api.MemoryHistoryPoint {
	oom, oomKill, high, maxEvents := memory.RecentEventCounts()
	return api.MemoryHistoryPoint{
		CapturedAt: capturedAt, TotalBytes: memory.TotalBytes, AnonBytes: memory.AnonBytes,
		FileCacheBytes: memory.FileCacheBytes(), ShmemBytes: memory.ShmemBytes,
		SlabReclaimableBytes: memory.SlabReclaimableBytes, SlabUnreclaimableBytes: memory.SlabUnreclaimableBytes,
		KernelBytes: memory.KernelBytes, SocketBytes: memory.SocketBytes, PageTableBytes: memory.PageTableBytes,
		FileMappedBytes: memory.FileMappedBytes, AnonTHPBytes: memory.AnonTHPBytes,
		FileTHPBytes: memory.FileTHPBytes, ShmemTHPBytes: memory.ShmemTHPBytes,
		ResidualBytes: memory.ResidualBytes(),
		SwapBytes:     memory.SwapCurrentBytes, PeakBytes: memory.PeakBytes,
		PSISomeAvg10: memory.PSISomeAvg10, PSIFullAvg10: memory.PSIFullAvg10,
		OOMEventsDelta: oom, OOMKillEventsDelta: oomKill, HighEventsDelta: high, MaxEventsDelta: maxEvents,
		ReclaimDeltasKnown: memory.ReclaimDeltasKnown,
		RefaultAnonDelta:   memory.RefaultAnonDelta, RefaultFileDelta: memory.RefaultFileDelta,
		PageScanDelta: memory.PageScanDelta, PageStealDelta: memory.PageStealDelta,
		MajorFaultsDelta: memory.MajorPageFaultsDelta,
	}
}

func historyKey(namespace, podName, podUID, nodeName string) string {
	return strings.Join([]string{namespace, podName, podUID, nodeName}, "\x00")
}
