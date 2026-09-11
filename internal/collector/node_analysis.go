package collector

import (
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/capability"
	"github.com/danushkastanley/kube-memlens/internal/nodeanalysis"
)

// AnalyseNode takes one immutable, UID-bound view. ClusterPods must only be
// requested after this request's cluster-wide Pod authorisation succeeds.
func (s *Store) AnalyseNode(name string, access nodeanalysis.Access, qualification *nodeanalysis.Qualification, rank nodeanalysis.Metric, limit int, now time.Time) (nodeanalysis.Analysis, bool, error) {
	s.mu.RLock()
	uid := s.expectedNodeUIDs[name]
	if uid == "" {
		s.mu.RUnlock()
		return nodeanalysis.Analysis{}, false, nil
	}
	input := nodeanalysis.Input{Now: now, NodeName: name, NodeUID: uid, Access: access, Qualification: qualification, Rank: rank, Limit: limit,
		SourceAvailability: capability.Unreported}
	entry := s.nodeContext.latest[name]
	input.Current = decodeNodeObservation(entry.good)
	if report := decodeNodeObservation(entry.report); report != nil {
		input.SourceAvailability = report.Availability
	}
	if series := s.nodeContext.history[uid]; series != nil && len(series.points) > 1 {
		input.Previous = decodeNodeObservation(series.points[len(series.points)-2].data)
	}
	if access == nodeanalysis.ClusterPods {
		frame := s.nodes[name]
		input.Cgroup = nodeanalysis.CgroupFrame{NodeUID: frame.uid, CapturedAt: frame.capturedAt, Coverage: nodeanalysis.Missing}
		if !frame.capturedAt.IsZero() {
			if len(frame.containers) > nodeanalysis.MaxContainers {
				s.mu.RUnlock()
				return nodeanalysis.Analysis{}, true, ErrReadPageTooLarge
			}
			input.Cgroup.Coverage = nodeanalysis.Complete
			if !s.nodeIdentityCurrentLocked(name, uid, now) || frame.environment.CgroupVersion != "v2" || frame.environment.CgroupReadErrors > 0 ||
				!frame.environment.WorkloadContextAvailable || frame.environment.WorkloadContextErrors > 0 {
				input.Cgroup.Coverage = nodeanalysis.Partial
			}
			if frame.uid == uid {
				input.Cgroup.Containers = make([]nodeanalysis.Container, len(frame.containers))
				for i, container := range frame.containers {
					input.Cgroup.Containers[i] = analysisContainer(container)
				}
			}
		}
	}
	s.mu.RUnlock()
	result, err := nodeanalysis.Analyse(input)
	return result, true, err
}

func analysisContainer(value api.ContainerSnapshot) nodeanalysis.Container {
	memory := value.Memory
	result := nodeanalysis.Container{ID: value.ContainerID, Namespace: value.Namespace, PodName: value.PodName, PodUID: value.PodUID, ContainerName: value.ContainerName,
		WorkloadKind: value.Context.WorkloadKind, WorkloadName: value.Context.WorkloadName,
		Charge:                nodeanalysis.Charges{Total: memory.TotalBytes, Anon: memory.AnonBytes, Cache: memory.FileCacheBytes(), Shmem: memory.ShmemBytes, Residual: memory.ResidualBytes()},
		CompositionConsistent: true}
	remaining := memory.TotalBytes
	for _, part := range []uint64{memory.AnonBytes, memory.FileCacheBytes(), memory.ShmemBytes} {
		if part > remaining {
			result.CompositionConsistent = false
			break
		}
		remaining -= part
	}
	if memory.PressureKnown {
		psi := memory.PSIFullAvg10
		result.PSIFullAvg10 = &psi
	}
	if value.DeltaWindowKnown {
		result.OOMWindowStartedAt = value.DeltaStartedAt
		switch {
		case memory.LocalEventsKnown && memory.LocalEventDeltasKnown:
			count := memory.LocalOOMKillEventsDelta
			result.OOMKills = &count
		case !memory.LocalEventsKnown && memory.EventDeltasKnown:
			count := memory.OOMKillEventsDelta
			result.OOMKills = &count
		}
	}
	return result
}
