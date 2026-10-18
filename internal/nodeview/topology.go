package nodeview

import (
	"fmt"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/memorytopology"
)

// TopologyLines keeps NUMA and HugeTLB values out of ordinary memory totals.
func TopologyLines(value api.NodeMemoryTopology, now time.Time, width int) []string {
	lines := []string{"", "NUMA and HugeTLB context (separate from ordinary memory):"}
	if value.Current == nil {
		return Wrap(append(lines, "Topology is not reported. Requires the optional profile and named Node permission."), width)
	}
	current, err := memorytopology.Analyse(value.Current.Observation, now)
	if err != nil {
		return Wrap(append(lines, "Topology source evidence is invalid."), width)
	}
	lines = append(lines, "NUMA distribution: "+current.NUMA.State, "HugeTLB pools: "+current.PoolState+"; cgroup counters: "+current.CgroupState)
	lines = append(lines, "NUMA source: "+sample(current.Observation.NUMA.CapturedAt, now), "HugeTLB source: "+sample(current.Observation.Pools.CapturedAt, now), "Cgroup source: "+sample(current.Observation.Cgroup.CapturedAt, now))
	for _, node := range current.Observation.NUMA.Items {
		lines = append(lines, fmt.Sprintf("NUMA %d: total %s; free %s", node.ID, bytes(node.TotalBytes), bytes(node.FreeBytes)))
		if p := node.Placement; p != nil {
			lines = append(lines, fmt.Sprintf("Cumulative placement pages: hit %s; miss %s; foreign %s; local %s; other %s; interleave %s", count(p.HitPages), count(p.MissPages), count(p.ForeignPages), count(p.LocalPages), count(p.OtherPages), count(p.InterleavePages)))
		}
	}
	for _, p := range current.Observation.Pools.Items {
		lines = append(lines, fmt.Sprintf("Page size %s source counts: total %s; free %s; reserved %s; surplus %s", bytes(&p.PageSizeBytes), count(p.TotalPages), count(p.FreePages), count(p.ReservedPages), count(p.SurplusPages)))
	}
	for _, p := range current.Pools {
		lines = append(lines, fmt.Sprintf("Page size %s: %s; pool %s; in use %s; reserved %s; free unreserved %s", bytes(&p.PageSizeBytes), p.State, bytes(p.PoolBytes), bytes(p.InUseBytes), bytes(p.ReservedBytes), bytes(p.FreeUnreservedBytes)))
	}
	for _, g := range current.Observation.Cgroup.Items {
		limit := "unreported"
		if g.Limit.Unlimited {
			limit = "unlimited"
		} else if g.Limit.Bytes != nil {
			limit = bytes(g.Limit.Bytes)
		}
		failures := "unreported"
		if g.LimitFailures != nil {
			failures = fmt.Sprint(*g.LimitFailures)
		}
		lines = append(lines, fmt.Sprintf("Cgroup page size %s: current %s; limit %s; cumulative limit failures %s", bytes(&g.PageSizeBytes), bytes(g.CurrentBytes), limit, failures))
		lines = append(lines, "Cgroup reservation-accounted bytes: "+bytes(g.ReservationBytes))
	}
	if value.LastGood != nil && (!value.LastGood.Observation.NUMA.CapturedAt.Equal(current.Observation.NUMA.CapturedAt) || !value.LastGood.Observation.Pools.CapturedAt.Equal(current.Observation.Pools.CapturedAt) || !value.LastGood.Observation.Cgroup.CapturedAt.Equal(current.Observation.Cgroup.CapturedAt)) {
		retained := value.LastGood.Observation
		lines = append(lines, "Retained sources are available in an explicit topology capture; their clocks were not refreshed:", "NUMA: "+sample(retained.NUMA.CapturedAt, now), "HugeTLB: "+sample(retained.Pools.CapturedAt, now), "Cgroup: "+sample(retained.Cgroup.CapturedAt, now))
	}
	lines = append(lines, current.Caveats...)
	return Wrap(lines, width)
}
