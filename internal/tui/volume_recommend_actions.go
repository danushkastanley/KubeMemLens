package tui

import (
	"context"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/client"
	"github.com/danushkastanley/kube-memlens/internal/explain"
	"github.com/danushkastanley/kube-memlens/internal/incident"
	"github.com/danushkastanley/kube-memlens/internal/recommend"
)

func (m *appModel) startVolumeRecommendation() tea.Cmd {
	ref, ok := m.currentActionRef()
	if !ok {
		return nil
	}
	request := actionRequest{kind: actionRecommend, ref: ref}
	if ref.kind == entityWorkload {
		reader, ok := m.client.(client.WorkloadVolumeReader)
		if !ok {
			m.setActionError(fmt.Errorf("workload volume recommendations require the authenticated Kubernetes API connection"))
			return nil
		}
		request.volumeWorkloadReader = reader
	} else {
		reader, ok := m.client.(incident.VolumeCaptureReader)
		if !ok {
			m.setActionError(fmt.Errorf("volume recommendations require the authenticated Kubernetes API connection"))
			return nil
		}
		pod, ok := m.currentActionPod()
		if !ok {
			return nil
		}
		request.volumeReader = reader
		request.volumeExpectedUID = pod.PodUID
	}
	return m.startAction(request)
}

func volumeRecommendationResult(ctx context.Context, request actionRequest) (actionResult, error) {
	var memory *explain.Result
	var volumes []explain.VolumeResult
	var caveats []string
	var pods []api.PodSnapshot
	if request.volumeWorkloadReader != nil {
		value, err := request.volumeWorkloadReader.WorkloadVolumes(ctx, request.ref.namespace, request.ref.workloadKind, request.ref.name)
		if err != nil {
			return actionResult{}, err
		}
		analysis := explain.AnalyzeWorkloadVolumes(value, time.Now().UTC())
		for _, pod := range analysis.Pods {
			volumes = append(volumes, pod.Analysis)
		}
		caveats = analysis.Caveats
		pods = value.Workload.Pods
		if len(value.Workload.Pods) > 0 {
			result := explain.AnalyzeWorkload(value.Workload)
			memory = &result
		}
	} else {
		pod, view, err := client.ReadPodVolumeEvidence(ctx, request.volumeReader, request.ref.namespace, request.ref.podName, request.volumeExpectedUID)
		if err != nil {
			return actionResult{}, err
		}
		now := time.Now().UTC()
		result := explain.AnalyzePodAt(pod, now)
		memory = &result
		analysis := explain.AnalyzeVolumes(explain.VolumeInput{Pod: pod, Volumes: view.Context, Now: now})
		volumes = []explain.VolumeResult{analysis}
		caveats = analysis.Caveats
		pods = []api.PodSnapshot{pod}
	}
	lines := []string{"Fresh authorised evidence; automatic mutation: disabled."}
	items := append(recommend.ForVolumeEvidence(memory, volumes), recommend.ForPodMemoryQoS(pods)...)
	for _, item := range items {
		lines = append(lines, "", item.Action, item.Rationale)
		for _, condition := range item.Conditions {
			lines = append(lines, "- "+condition)
		}
	}
	for _, caveat := range caveats {
		lines = append(lines, "- "+caveat)
	}
	return actionResult{title: "Volume-aware recommendations", lines: lines}, nil
}
