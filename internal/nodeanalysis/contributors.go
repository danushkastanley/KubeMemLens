package nodeanalysis

import (
	"math"
	"slices"
	"sort"
	"strings"

	"github.com/danushkastanley/kube-memlens/internal/capability"
)

func aggregateContributors(input Input, result *Analysis) {
	coverage := &ContributorCoverage{Source: capability.Cgroup, ObservedChargeFormula: "sum(mapped container memory.current)", State: input.Cgroup.Coverage, CapturedAt: input.Cgroup.CapturedAt}
	result.Coverage = coverage
	if coverage.State == "" || coverage.State == Missing {
		coverage.State = Missing
		addCaveat(result, AgentMissing)
		result.Confidence = Low
		return
	}
	if input.Cgroup.NodeUID != input.NodeUID {
		coverage.State = Partial
		addCaveat(result, IdentityMismatch)
		result.Confidence = Low
		return
	}
	if !fresh(input.Cgroup.CapturedAt, input.Now, CgroupStaleAfter) {
		coverage.State = Partial
		addCaveat(result, AgentStale)
		result.Confidence = Low
	}
	if coverage.State != Complete {
		addCaveat(result, AgentPartial)
		result.Confidence = Low
	}
	pods := map[string]Contributor{}
	workloads := map[string]Contributor{}
	seen := map[string]bool{}
	overflow := false
	var total uint64
	for _, container := range input.Cgroup.Containers {
		if container.ID == "" || seen[container.ID] {
			coverage.State = Partial
			addCaveat(result, AgentPartial)
			continue
		}
		seen[container.ID] = true
		if container.Namespace == "" || container.PodName == "" || container.PodUID == "" || container.ContainerName == "" {
			coverage.UnmappedContainers++
			coverage.State = Partial
			continue
		}
		coverage.MappedContainers++
		if !container.CompositionConsistent {
			coverage.State = Partial
			addCaveat(result, CompositionOverlap)
		}
		sum, ok := add(total, container.Charge.Total)
		if !ok {
			overflow = true
		}
		total = sum
		psi := container.PSIFullAvg10
		if psi != nil && (math.IsNaN(*psi) || math.IsInf(*psi, 0) || *psi < 0 || *psi > 100) {
			psi = nil
			coverage.State = Partial
		}
		oom := container.OOMKills
		if !fresh(container.OOMWindowStartedAt, input.Now, CgroupStaleAfter) || !input.Cgroup.CapturedAt.After(container.OOMWindowStartedAt) {
			oom = nil
		}
		pod := Contributor{Namespace: container.Namespace, Name: container.PodName, UID: container.PodUID, Kind: "Pod", Charge: container.Charge, PSIFullAvg10: psi, OOMKills: oom}
		key := strings.Join([]string{pod.Namespace, pod.Name, pod.UID}, "\x00")
		combined, ok := combine(pods[key], pod)
		pods[key] = combined
		if !ok {
			overflow = true
		}
		workload := pod
		workload.Kind, workload.Name = container.WorkloadKind, container.WorkloadName
		if workload.Kind == "" || workload.Name == "" {
			workload.Kind, workload.Name = "Pod", container.PodName
		} else {
			workload.UID = ""
		}
		key = strings.Join([]string{workload.Namespace, workload.Kind, workload.Name, workload.UID}, "\x00")
		combined, ok = combine(workloads[key], workload)
		workloads[key] = combined
		if !ok {
			overflow = true
		}
	}
	coverage.Pods = len(pods)
	if coverage.State != Complete {
		result.Confidence = Low
	}
	if coverage.UnmappedContainers > 0 {
		addCaveat(result, Unmapped)
		result.Confidence = Low
	}
	if overflow {
		addCaveat(result, Overflow)
		coverage.State = Partial
		result.Confidence = Low
		return
	}
	result.ObservedPodCharge = &total
	result.Rankings = &Rankings{Source: capability.Cgroup, Formula: rankingFormula(input.Rank), Metric: input.Rank, Limit: input.Limit, Truncated: len(pods) > input.Limit || len(workloads) > input.Limit,
		Pods: ranked(pods, input.Rank, input.Limit), Workloads: ranked(workloads, input.Rank, input.Limit)}
	if fresh(input.Cgroup.CapturedAt, input.Now, CgroupStaleAfter) {
		kills, overflow := observedOOMKills(pods)
		if overflow {
			addCaveat(result, Overflow)
			result.Confidence = Low
		}
		if overflow || kills != nil && *kills > 0 {
			result.Signals = append(result.Signals, Signal{Code: "observed-pod-oom-kills", Source: capability.Cgroup,
				CapturedAt: input.Cgroup.CapturedAt, Severity: Warning, Count: kills, Unit: "events"})
			promote(result, Warning)
		}
	}

}

func combine(previous, current Contributor) (Contributor, bool) {
	if previous.Name == "" {
		current.PSIFullAvg10 = copyFloat(current.PSIFullAvg10)
		current.OOMKills = copyUint(current.OOMKills)
		return current, true
	}
	success := true
	sums := []struct {
		target *uint64
		value  uint64
	}{
		{&previous.Charge.Total, current.Charge.Total}, {&previous.Charge.Anon, current.Charge.Anon}, {&previous.Charge.Cache, current.Charge.Cache},
		{&previous.Charge.Shmem, current.Charge.Shmem}, {&previous.Charge.Residual, current.Charge.Residual},
	}
	for _, sum := range sums {
		value, ok := add(*sum.target, sum.value)
		*sum.target = value
		success = success && ok
	}
	if current.PSIFullAvg10 != nil && (previous.PSIFullAvg10 == nil || *current.PSIFullAvg10 > *previous.PSIFullAvg10) {
		previous.PSIFullAvg10 = copyFloat(current.PSIFullAvg10)
	}
	if current.OOMKills != nil {
		if previous.OOMKills == nil {
			previous.OOMKills = copyUint(current.OOMKills)
		} else {
			value, ok := add(*previous.OOMKills, *current.OOMKills)
			previous.OOMKills = &value
			success = success && ok
		}
	}
	return previous, success
}

func ranked(values map[string]Contributor, metric Metric, limit int) []Contributor {
	result := make([]Contributor, 0, len(values))
	for _, value := range values {
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool {
		left, right := result[i], result[j]
		switch metric {
		case PSI:
			if (left.PSIFullAvg10 == nil) != (right.PSIFullAvg10 == nil) {
				return left.PSIFullAvg10 != nil
			}
			if left.PSIFullAvg10 != nil && *left.PSIFullAvg10 != *right.PSIFullAvg10 {
				return *left.PSIFullAvg10 > *right.PSIFullAvg10
			}
		case OOM:
			if (left.OOMKills == nil) != (right.OOMKills == nil) {
				return left.OOMKills != nil
			}
			if left.OOMKills != nil && *left.OOMKills != *right.OOMKills {
				return *left.OOMKills > *right.OOMKills
			}
		default:
			a, b := chargeMetric(left.Charge, metric), chargeMetric(right.Charge, metric)
			if a != b {
				return a > b
			}
		}
		return strings.Join([]string{left.Namespace, left.Kind, left.Name, left.UID}, "\x00") < strings.Join([]string{right.Namespace, right.Kind, right.Name, right.UID}, "\x00")
	})
	return slices.Clone(result[:min(limit, len(result))])
}

func chargeMetric(charge Charges, metric Metric) uint64 {
	switch metric {
	case Anon:
		return charge.Anon
	case Cache:
		return charge.Cache
	case Shmem:
		return charge.Shmem
	case Residual:
		return charge.Residual
	default:
		return charge.Total
	}
}

func add(left, right uint64) (uint64, bool) {
	if right > math.MaxUint64-left {
		return 0, false
	}
	return left + right, true
}

func observedOOMKills(pods map[string]Contributor) (*uint64, bool) {
	total := uint64(0)
	for _, pod := range pods {
		if pod.OOMKills == nil {
			continue
		}
		value, ok := add(total, *pod.OOMKills)
		if !ok {
			return nil, true
		}
		total = value
	}
	return &total, false
}

func rankingFormula(metric Metric) string {
	switch metric {
	case Anon:
		return "sum(container memory.stat.anon)"
	case Cache:
		return "sum(max(0, container memory.stat.file - container memory.stat.shmem))"
	case Shmem:
		return "sum(container memory.stat.shmem)"
	case Residual:
		return "sum(max(0, container memory.current - anon - fileCache - shmem))"
	case PSI:
		return "max(observed container memory.pressure.full avg10)"
	case OOM:
		return "sum(observed container OOM kill deltas in known windows)"
	default:
		return "sum(container memory.current)"
	}
}
