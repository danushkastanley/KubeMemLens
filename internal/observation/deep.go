package observation

import (
	"maps"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/capability"
)

func FromDeepPod(snapshot api.PodSnapshot, receivedAt time.Time) Pod {
	context := snapshot.Context
	context.Labels = maps.Clone(context.Labels)
	pod := Pod{Namespace: snapshot.Namespace, Name: snapshot.PodName, UID: snapshot.PodUID, NodeName: snapshot.NodeName,
		Context: context, Cgroup: &Cgroup{Memory: snapshot.Memory, Evidence: deepEnvelope(snapshot.CapturedAt, receivedAt, snapshot.Freshness, snapshot.Completeness, capability.PodScope)}}
	for _, value := range snapshot.Containers {
		context := value.Context
		context.Labels = maps.Clone(context.Labels)
		pod.Containers = append(pod.Containers, Container{Name: value.ContainerName, Context: context, Cgroup: &Cgroup{
			Memory: value.Memory, ContainerID: value.ContainerID, CgroupPath: value.CgroupPath,
			DeltaStartedAt: value.DeltaStartedAt, DeltaWindowKnown: value.DeltaWindowKnown,
			Evidence: deepEnvelope(value.CapturedAt, receivedAt, value.Freshness, value.Completeness, capability.ContainerScope),
		}})
	}
	return pod
}

// DeepSnapshot reconstructs only actual cgroup evidence. A restricted Pod can
// never become an apparently healthy zero-valued collector snapshot.
func (pod Pod) DeepSnapshot() (api.PodSnapshot, bool) {
	if pod.Cgroup == nil {
		return api.PodSnapshot{}, false
	}
	snapshot := api.PodSnapshot{Namespace: pod.Namespace, PodName: pod.Name, PodUID: pod.UID, NodeName: pod.NodeName,
		Context: pod.Context, Memory: pod.Cgroup.Memory, CapturedAt: pod.Cgroup.Evidence.CapturedAt,
		Freshness: api.EvidenceFreshness(pod.Cgroup.Evidence.Freshness), Completeness: api.EvidenceCompleteness(pod.Cgroup.Evidence.Completeness)}
	snapshot.Context.Labels = maps.Clone(snapshot.Context.Labels)
	for _, value := range pod.Containers {
		if value.Cgroup == nil {
			return api.PodSnapshot{}, false
		}
		cgroup := value.Cgroup
		context := value.Context
		context.Labels = maps.Clone(context.Labels)
		snapshot.Containers = append(snapshot.Containers, api.ContainerSnapshot{Namespace: pod.Namespace, PodName: pod.Name, PodUID: pod.UID,
			ContainerName: value.Name, NodeName: pod.NodeName, ContainerID: cgroup.ContainerID, CgroupPath: cgroup.CgroupPath,
			Context: context, Memory: cgroup.Memory, CapturedAt: cgroup.Evidence.CapturedAt,
			Freshness: api.EvidenceFreshness(cgroup.Evidence.Freshness), Completeness: api.EvidenceCompleteness(cgroup.Evidence.Completeness),
			DeltaStartedAt: cgroup.DeltaStartedAt, DeltaWindowKnown: cgroup.DeltaWindowKnown})
	}
	return snapshot, true
}

func deepEnvelope(capturedAt, receivedAt time.Time, freshness api.EvidenceFreshness, completeness api.EvidenceCompleteness, scope capability.Scope) capability.Envelope {
	return capability.Envelope{Source: capability.Cgroup, CapturedAt: capturedAt, ReceivedAt: receivedAt, Scope: scope,
		Freshness: capability.Freshness(freshness), Completeness: capability.Completeness(completeness), Stability: capability.Stable}
}
