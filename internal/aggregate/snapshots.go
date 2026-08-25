package aggregate

import (
	"sort"
	"strings"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/model"
)

// Pods builds deterministic Pod snapshots from container-level observations.
func Pods(containers []api.ContainerSnapshot) []api.PodSnapshot {
	byPod := map[string]*api.PodSnapshot{}
	order := []string{}
	for _, container := range containers {
		if container.Namespace == "" || container.PodName == "" {
			continue
		}
		key := strings.Join([]string{container.Namespace, container.PodUID, container.PodName, container.NodeName}, "\x00")
		pod := byPod[key]
		if pod == nil {
			pod = &api.PodSnapshot{
				Namespace: container.Namespace,
				PodName:   container.PodName,
				PodUID:    container.PodUID,
				NodeName:  container.NodeName,
			}
			byPod[key] = pod
			order = append(order, key)
		}
		pod.Containers = append(pod.Containers, container)
		if container.CapturedAt.After(pod.CapturedAt) {
			pod.CapturedAt = container.CapturedAt
		}
	}

	pods := make([]api.PodSnapshot, 0, len(byPod))
	for _, key := range order {
		pod := *byPod[key]
		memories := make([]model.MemoryBreakdown, 0, len(pod.Containers))
		for _, container := range pod.Containers {
			memories = append(memories, container.Memory)
			addContainerContext(&pod.Context, container.Context)
		}
		pod.Memory = model.SumMemory(pod.Namespace+"/"+pod.PodName, memories)
		pod.Freshness, pod.Completeness = containerEvidence(pod.Containers)
		sortContainers(pod.Containers)
		pods = append(pods, pod)
	}
	sort.Slice(pods, func(i, j int) bool {
		if pods[i].Namespace == pods[j].Namespace {
			return pods[i].PodName < pods[j].PodName
		}
		return pods[i].Namespace < pods[j].Namespace
	})
	return pods
}

// Namespaces rolls Pod snapshots up to namespace summaries.
func Namespaces(pods []api.PodSnapshot) []api.NamespaceSnapshot {
	byNamespace := map[string]*api.NamespaceSnapshot{}
	for _, pod := range pods {
		ns := byNamespace[pod.Namespace]
		if ns == nil {
			ns = &api.NamespaceSnapshot{Namespace: pod.Namespace}
			byNamespace[pod.Namespace] = ns
		}
		ns.PodCount++
		ns.Memory = model.AddMemory(ns.Memory, pod.Memory)
		mergeEvidence(&ns.Freshness, &ns.Completeness, pod.Freshness, pod.Completeness)
		ns.Memory.Name = pod.Namespace
		if pod.CapturedAt.After(ns.CapturedAt) {
			ns.CapturedAt = pod.CapturedAt
		}
	}
	items := make([]api.NamespaceSnapshot, 0, len(byNamespace))
	for _, namespace := range byNamespace {
		items = append(items, *namespace)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Namespace < items[j].Namespace })
	return items
}

// Workloads rolls Pod snapshots up to top-level workload summaries while
// retaining replica evidence for explanations and selector evaluation.
func Workloads(pods []api.PodSnapshot) []api.WorkloadSnapshot {
	byWorkload := map[string]*api.WorkloadSnapshot{}
	keys := []string{}
	for _, pod := range pods {
		kind, name := podWorkload(pod)
		key := strings.Join([]string{pod.Namespace, kind, name}, "\x00")
		workload := byWorkload[key]
		if workload == nil {
			workload = &api.WorkloadSnapshot{Namespace: pod.Namespace, Kind: kind, Name: name}
			byWorkload[key] = workload
			keys = append(keys, key)
		}
		workload.Pods = append(workload.Pods, pod)
		if pod.CapturedAt.After(workload.CapturedAt) {
			workload.CapturedAt = pod.CapturedAt
		}
		if workload.LargestPodName == "" || pod.Memory.TotalBytes > workload.LargestPodBytes {
			workload.LargestPodBytes = pod.Memory.TotalBytes
			workload.LargestPodName = pod.PodName
		}
	}
	sort.Strings(keys)
	items := make([]api.WorkloadSnapshot, 0, len(keys))
	for _, key := range keys {
		workload := *byWorkload[key]
		workload.PodCount = len(workload.Pods)
		memories := make([]model.MemoryBreakdown, 0, len(workload.Pods))
		for _, pod := range workload.Pods {
			memories = append(memories, pod.Memory)
		}
		workload.Memory = model.SumMemory(workload.Namespace+"/"+workload.Kind+"/"+workload.Name, memories)
		for _, pod := range workload.Pods {
			mergeEvidence(&workload.Freshness, &workload.Completeness, pod.Freshness, pod.Completeness)
		}
		normaliseWorkloadBoundaries(&workload.Memory, workload.Pods)
		sort.Slice(workload.Pods, func(i, j int) bool {
			return workload.Pods[i].Memory.TotalBytes > workload.Pods[j].Memory.TotalBytes
		})
		items = append(items, workload)
	}
	return items
}

func containerEvidence(containers []api.ContainerSnapshot) (api.EvidenceFreshness, api.EvidenceCompleteness) {
	fresh, stale, partial := false, false, false
	for _, container := range containers {
		fresh = fresh || container.Freshness != api.EvidenceFreshnessStale
		stale = stale || container.Freshness == api.EvidenceFreshnessStale
		partial = partial || container.Completeness == api.EvidencePartial
	}
	if stale && !fresh {
		if partial {
			return api.EvidenceFreshnessStale, api.EvidencePartial
		}
		return api.EvidenceFreshnessStale, api.EvidenceComplete
	}
	if stale || partial {
		return api.EvidenceFreshnessFresh, api.EvidencePartial
	}
	return api.EvidenceFreshnessFresh, api.EvidenceComplete
}

func mergeEvidence(freshness *api.EvidenceFreshness, completeness *api.EvidenceCompleteness, nextFreshness api.EvidenceFreshness, nextCompleteness api.EvidenceCompleteness) {
	if *freshness == "" {
		*freshness, *completeness = nextFreshness, nextCompleteness
		return
	}
	if *freshness != nextFreshness || nextCompleteness == api.EvidencePartial {
		*completeness = api.EvidencePartial
	}
	if *freshness == api.EvidenceFreshnessStale && nextFreshness == api.EvidenceFreshnessFresh {
		*freshness = api.EvidenceFreshnessFresh
	}
}

func addContainerContext(pod *api.PodContext, container api.ContainerContext) {
	if container.MemoryRequestKnown {
		pod.MemoryRequestBytes += container.MemoryRequestBytes
		pod.MemoryRequestContainers++
	}
	if container.MemoryLimitKnown {
		pod.MemoryLimitBytes += container.MemoryLimitBytes
		pod.MemoryLimitContainers++
	}
	pod.RestartCount += container.RestartCount
	if container.LastTerminationKnown && (!pod.LastTerminationKnown || container.LastTerminationFinishedAt.After(pod.LastTerminationFinishedAt)) {
		pod.LastTerminationKnown = true
		pod.LastTerminationReason = container.LastTerminationReason
		pod.LastTerminationExitCode = container.LastTerminationExitCode
		pod.LastTerminationFinishedAt = container.LastTerminationFinishedAt
	}
	setFirst(&pod.QoSClass, container.QoSClass)
	setFirst(&pod.Phase, container.PodPhase)
	if pod.CreatedAt.IsZero() || (!container.PodCreatedAt.IsZero() && container.PodCreatedAt.Before(pod.CreatedAt)) {
		pod.CreatedAt = container.PodCreatedAt
	}
	if pod.OwnerKind == "" {
		pod.OwnerKind, pod.OwnerName = container.OwnerKind, container.OwnerName
	}
	if pod.WorkloadKind == "" {
		pod.WorkloadKind, pod.WorkloadName = container.WorkloadKind, container.WorkloadName
	}
	copyLabels(pod, container.Labels)
	setFirst(&pod.NodeMemoryPressure, container.NodeMemoryPressure)
	if !pod.NodeMemoryAllocatableKnown && container.NodeMemoryAllocatableKnown {
		pod.NodeMemoryAllocatableKnown = true
		pod.NodeMemoryAllocatable = container.NodeMemoryAllocatable
	}
	setFirst(&pod.RuntimeClassName, container.RuntimeClassName)
	if pod.MemoryEmptyDirCount == 0 && container.MemoryEmptyDirCount > 0 {
		pod.MemoryEmptyDirCount = container.MemoryEmptyDirCount
		pod.MemoryEmptyDirLimited = container.MemoryEmptyDirLimited
		pod.MemoryEmptyDirLimitBytes = container.MemoryEmptyDirLimitBytes
	}
}

func copyLabels(pod *api.PodContext, labels map[string]string) {
	if pod.Labels != nil || len(labels) == 0 {
		return
	}
	pod.Labels = make(map[string]string, len(labels))
	for key, value := range labels {
		pod.Labels[key] = value
	}
}

func setFirst(target *string, value string) {
	if *target == "" {
		*target = value
	}
}

func normaliseWorkloadBoundaries(memory *model.MemoryBreakdown, pods []api.PodSnapshot) {
	memory.PeakKnown = false
	for _, pod := range pods {
		memory.MinKnown = memory.MinKnown && pod.Memory.MinKnown
		memory.LowKnown = memory.LowKnown && pod.Memory.LowKnown
		memory.HighKnown = memory.HighKnown && pod.Memory.HighKnown
		memory.MaxKnown = memory.MaxKnown && pod.Memory.MaxKnown
		memory.SwapMaxKnown = memory.SwapMaxKnown && pod.Memory.SwapMaxKnown
	}
}

func podWorkload(pod api.PodSnapshot) (kind, name string) {
	if pod.Context.WorkloadKind != "" && pod.Context.WorkloadName != "" {
		return pod.Context.WorkloadKind, pod.Context.WorkloadName
	}
	if pod.Context.OwnerKind != "" && pod.Context.OwnerName != "" {
		return pod.Context.OwnerKind, pod.Context.OwnerName
	}
	return "Pod", pod.PodName
}

func sortContainers(items []api.ContainerSnapshot) {
	sort.Slice(items, func(i, j int) bool {
		left, right := items[i], items[j]
		if left.Namespace != right.Namespace {
			return left.Namespace < right.Namespace
		}
		if left.PodName != right.PodName {
			return left.PodName < right.PodName
		}
		if left.ContainerName != right.ContainerName {
			return left.ContainerName < right.ContainerName
		}
		return left.ContainerID < right.ContainerID
	})
}
