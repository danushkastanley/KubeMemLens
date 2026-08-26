package aggregate

import (
	"sort"
	"strings"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/model"
)

type podSummaryState struct {
	pod     api.PodSnapshot
	fresh   bool
	stale   bool
	partial bool
}

// PodSummaryAccumulator builds Pod aggregates without retaining nested
// container evidence. Callers can stream a bounded authorised selection into it.
type PodSummaryAccumulator struct {
	byPod map[string]*podSummaryState
	order []string
}

func NewPodSummaryAccumulator() *PodSummaryAccumulator {
	return &PodSummaryAccumulator{byPod: map[string]*podSummaryState{}}
}

func (a *PodSummaryAccumulator) Add(container api.ContainerSnapshot) {
	if container.Namespace == "" || container.PodName == "" {
		return
	}
	key := strings.Join([]string{container.Namespace, container.PodUID, container.PodName, container.NodeName}, "\x00")
	state := a.byPod[key]
	if state == nil {
		state = &podSummaryState{pod: api.PodSnapshot{
			Namespace: container.Namespace,
			PodName:   container.PodName,
			PodUID:    container.PodUID,
			NodeName:  container.NodeName,
		}}
		a.byPod[key] = state
		a.order = append(a.order, key)
	}
	state.pod.Memory = model.AddMemory(state.pod.Memory, container.Memory)
	addContainerContext(&state.pod.Context, container.Context)
	state.fresh = state.fresh || container.Freshness != api.EvidenceFreshnessStale
	state.stale = state.stale || container.Freshness == api.EvidenceFreshnessStale
	state.partial = state.partial || container.Completeness == api.EvidencePartial
	if container.CapturedAt.After(state.pod.CapturedAt) {
		state.pod.CapturedAt = container.CapturedAt
	}
}

func (a *PodSummaryAccumulator) Snapshots() []api.PodSnapshot {
	pods := make([]api.PodSnapshot, 0, len(a.byPod))
	for _, key := range a.order {
		state := a.byPod[key]
		state.pod.Memory.Name = state.pod.Namespace + "/" + state.pod.PodName
		switch {
		case state.stale && !state.fresh:
			state.pod.Freshness = api.EvidenceFreshnessStale
			state.pod.Completeness = api.EvidenceComplete
			if state.partial {
				state.pod.Completeness = api.EvidencePartial
			}
		case state.stale || state.partial:
			state.pod.Freshness = api.EvidenceFreshnessFresh
			state.pod.Completeness = api.EvidencePartial
		default:
			state.pod.Freshness = api.EvidenceFreshnessFresh
			state.pod.Completeness = api.EvidenceComplete
		}
		pods = append(pods, state.pod)
	}
	sort.Slice(pods, func(i, j int) bool {
		if pods[i].Namespace == pods[j].Namespace {
			return pods[i].PodName < pods[j].PodName
		}
		return pods[i].Namespace < pods[j].Namespace
	})
	return pods
}
