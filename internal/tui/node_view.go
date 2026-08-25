package tui

import (
	"sort"
	"strings"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/model"
)

type nodeView struct {
	name           string
	capturedAt     time.Time
	stale          bool
	freshness      api.EvidenceFreshness
	podCount       int
	containerCount int
	memory         model.MemoryBreakdown
	environment    api.NodeEnvironment
}

func buildNodeViews(nodes []api.NodeSnapshotStatus, pods []api.PodSnapshot, query string) []nodeView {
	byName := make(map[string]*nodeView, len(nodes))
	for _, node := range nodes {
		byName[node.NodeName] = &nodeView{
			name:           node.NodeName,
			capturedAt:     node.CapturedAt,
			stale:          node.Stale,
			freshness:      node.Freshness,
			containerCount: node.ContainerCount,
			environment:    node.Environment,
		}
	}
	for _, pod := range pods {
		node := byName[pod.NodeName]
		if node == nil {
			node = &nodeView{name: pod.NodeName, capturedAt: pod.CapturedAt}
			byName[pod.NodeName] = node
		}
		node.podCount++
		node.memory = model.AddMemory(node.memory, pod.Memory)
		if pod.CapturedAt.After(node.capturedAt) {
			node.capturedAt = pod.CapturedAt
		}
	}
	query = strings.ToLower(strings.TrimSpace(query))
	items := make([]nodeView, 0, len(byName))
	for _, node := range byName {
		if node.name == "" {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(node.name+" "+node.environment.MemoryPressureStatus), query) {
			continue
		}
		items = append(items, *node)
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].memory.TotalBytes == items[j].memory.TotalBytes {
			return items[i].name < items[j].name
		}
		return items[i].memory.TotalBytes > items[j].memory.TotalBytes
	})
	return items
}

func nodePressureLabel(node nodeView) string {
	if node.freshness == api.EvidenceFreshnessMissing {
		return "missing"
	}
	if node.stale {
		return "stale"
	}
	if !node.environment.NodeContextAvailable {
		return "unknown"
	}
	if node.environment.MemoryPressureStatus == "True" {
		return "PRESSURE"
	}
	if node.environment.MemoryPressureStatus == "False" {
		return "clear"
	}
	return node.environment.MemoryPressureStatus
}
