package tui

import (
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/memorytopology"
	"github.com/danushkastanley/kube-memlens/internal/nodeview"
)

func topologyMatches(value *api.NodeMemoryTopology, uid string) bool {
	if value == nil || uid == "" {
		return false
	}
	for _, report := range []*memorytopology.Report{value.Current, value.LastGood} {
		if report != nil && report.Observation.NodeUID != uid {
			return false
		}
	}
	return true
}

func (m appModel) nodeTopologyLines(width int) []string {
	lines := []string{"", "NUMA and HugeTLB context (separate from ordinary memory):"}
	s := m.selectedNode
	if s.topologyErr != nil {
		return nodeview.Wrap(append(lines, "Topology unavailable: "+s.topologyErr.Error()), width)
	}
	if s.topology == nil || s.topology.Current == nil {
		return nodeview.Wrap(append(lines, "Topology is not reported. Requires the optional profile and named Node permission."), width)
	}
	return nodeview.TopologyLines(*s.topology, time.Now().UTC(), width)
}
