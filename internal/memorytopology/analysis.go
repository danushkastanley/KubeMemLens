package memorytopology

import (
	"cmp"
	"encoding/json"
	"slices"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/capability"
)

type NUMAUse struct {
	ID              uint16   `json:"id"`
	State           string   `json:"state"`
	NonFreeBytes    *uint64  `json:"nonFreeBytes,omitempty"`
	NonFreeFraction *float64 `json:"nonFreeFraction,omitempty"`
}

type Distribution struct {
	State                string    `json:"state"`
	Nodes                []NUMAUse `json:"nodes"`
	LeastOccupiedID      *uint16   `json:"leastOccupiedID,omitempty"`
	MostOccupiedID       *uint16   `json:"mostOccupiedID,omitempty"`
	FractionSpread       *float64  `json:"fractionSpread,omitempty"`
	EstimatedExcessBytes *float64  `json:"estimatedExcessBytes,omitempty"`
}

type PoolUse struct {
	PageSizeBytes       uint64  `json:"pageSizeBytes"`
	State               string  `json:"state"`
	PoolBytes           *uint64 `json:"poolBytes,omitempty"`
	InUseBytes          *uint64 `json:"inUseBytes,omitempty"`
	ReservedBytes       *uint64 `json:"reservedBytes,omitempty"`
	FreeUnreservedBytes *uint64 `json:"freeUnreservedBytes,omitempty"`
}

type Report struct {
	SchemaVersion int          `json:"schemaVersion"`
	PolicyVersion int          `json:"policyVersion"`
	ObservedAt    time.Time    `json:"observedAt"`
	Observation   Observation  `json:"observation"`
	NUMA          Distribution `json:"numa"`
	PoolState     string       `json:"poolState"`
	Pools         []PoolUse    `json:"pools"`
	CgroupState   string       `json:"cgroupState"`
	Caveats       []string     `json:"caveats"`
}

// Analyse produces informational context only. Its input has no ordinary memory
// total or kubelet available field to adjust, and no remediation/severity output.
func Analyse(o Observation, now time.Time) (Report, error) {
	if err := o.Validate(now, 0); err != nil {
		return Report{}, err
	}
	encoded, err := json.Marshal(o)
	if err != nil {
		return Report{}, ErrInvalid
	}
	var copy Observation
	if err := json.Unmarshal(encoded, &copy); err != nil {
		return Report{}, err
	}
	slices.SortFunc(copy.NUMA.Items, func(a, b NUMANode) int { return int(a.ID) - int(b.ID) })
	slices.SortFunc(copy.Pools.Items, func(a, b HugePool) int { return cmp.Compare(a.PageSizeBytes, b.PageSizeBytes) })
	for i := range copy.NUMA.Items {
		slices.SortFunc(copy.NUMA.Items[i].Pools, func(a, b HugePool) int { return cmp.Compare(a.PageSizeBytes, b.PageSizeBytes) })
	}
	slices.SortFunc(copy.Cgroup.Items, func(a, b HugeCgroup) int { return cmp.Compare(a.PageSizeBytes, b.PageSizeBytes) })
	for i := range copy.Cgroup.Items {
		slices.SortFunc(copy.Cgroup.Items[i].NUMA, func(a, b DomainBytes) int { return cmp.Compare(a.ID, b.ID) })
	}
	result := Report{SchemaVersion: 1, PolicyVersion: 1, ObservedAt: now.UTC(), Observation: copy, Pools: []PoolUse{}, Caveats: []string{
		"Topology context is informational; ordinary memory totals and diagnosis severity are unchanged.",
		"Non-free NUMA memory includes cache, kernel memory and hugepage pools; uneven distribution is not proof of pressure or access latency.",
		"NUMA placement counters are cumulative pages, not measured remote-memory traffic.",
		"HugeTLB pool total includes surplus. Reservations are outstanding commitments, separate from current in-use pages.",
		"Cgroup HugeTLB failures count limit failures, not global pool exhaustion or Kubernetes evictions.",
		"Kubelet availability is shown unchanged elsewhere; it may already exclude hugepage capacity.",
		"Topology Manager, Memory Manager and the running HugepageAwareEviction configuration are unreported.",
		"Requires readable Linux NUMA/HugeTLB sysfs and cgroup v2 HugeTLB files; absent root-cgroup counters are not zero.",
	}}
	result.NUMA = analyseNUMA(copy.NUMA, now)
	result.PoolState = sectionState(copy.Pools, now)
	result.CgroupState = sectionState(copy.Cgroup, now)
	if result.PoolState == "observed" || result.PoolState == "partial" {
		for _, pool := range copy.Pools.Items {
			result.Pools = append(result.Pools, analysePool(pool))
		}
	}
	encoded, err = json.Marshal(result)
	if err != nil {
		return Report{}, ErrInvalid
	}
	if len(encoded) > MaxReportBytes {
		return Report{}, ErrBounds
	}
	return result, nil
}

func sectionState[T any](s Section[T], now time.Time) string {
	if s.Availability != capability.Available {
		return string(s.Availability)
	}
	if now.Sub(s.CapturedAt) > FreshFor {
		return "stale"
	}
	if s.Reason == SourceChanged {
		return "source-changed"
	}
	if s.Completeness != capability.Complete {
		return "partial"
	}
	return "observed"
}

func analysePool(p HugePool) PoolUse {
	result := PoolUse{PageSizeBytes: p.PageSizeBytes, State: "unreported"}
	if p.TotalPages == nil || p.FreePages == nil {
		return result
	}
	if *p.FreePages > *p.TotalPages || (p.SurplusPages != nil && *p.SurplusPages > *p.TotalPages) || (p.ReservedPages != nil && *p.ReservedPages > *p.FreePages) {
		result.State = "inconsistent"
		return result
	}
	total, ok := PageBytes(*p.TotalPages, p.PageSizeBytes)
	if !ok {
		result.State = "overflow"
		return result
	}
	used, _ := PageBytes(*p.TotalPages-*p.FreePages, p.PageSizeBytes)
	result.State = "partial"
	result.PoolBytes = &total
	result.InUseBytes = &used
	if p.ReservedPages != nil {
		reserved, _ := PageBytes(*p.ReservedPages, p.PageSizeBytes)
		remaining, _ := PageBytes(*p.FreePages-*p.ReservedPages, p.PageSizeBytes)
		result.ReservedBytes = &reserved
		result.FreeUnreservedBytes = &remaining
	}
	if p.ReservedPages != nil && p.SurplusPages != nil {
		result.State = "observed"
	}
	return result
}
