package tui

import (
	"context"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/danushkastanley/kube-memlens/internal/client"
	"github.com/danushkastanley/kube-memlens/internal/explain"
	"github.com/danushkastanley/kube-memlens/internal/incident"
	"github.com/danushkastanley/kube-memlens/internal/volumeview"
)

func (m *appModel) startVolumeCompare() tea.Cmd {
	reader, ok := m.client.(incident.VolumeCaptureReader)
	if !ok {
		m.setActionError(fmt.Errorf("volume comparison requires the authenticated Kubernetes API connection"))
		return nil
	}
	pod, ok := m.currentActionPod()
	if !ok {
		m.setActionError(fmt.Errorf("select a Pod for volume comparison"))
		return nil
	}
	ref, _ := m.currentActionRef()
	before := m.action.volumeCompareSource
	m.action.volumeCompareSource = nil
	return m.startAction(actionRequest{kind: actionCompare, ref: ref, volumeReader: reader, volumeExpectedUID: pod.PodUID, volumeBefore: before})
}

func volumeCompareResult(ctx context.Context, request actionRequest) (actionResult, error) {
	if before := request.volumeBefore; before != nil {
		if _, _, err := client.ReadPodVolumeEvidence(ctx, request.volumeReader, before.Pod.Namespace, before.Pod.PodName, before.Pod.PodUID); err != nil {
			return actionResult{}, err
		}
	}
	pod, volumes, err := client.ReadPodVolumeEvidence(ctx, request.volumeReader, request.ref.namespace, request.ref.podName, request.volumeExpectedUID)
	if err != nil {
		return actionResult{}, err
	}
	after := explain.VolumeInput{Pod: pod, Volumes: volumes.Context, Now: time.Now().UTC()}
	if request.volumeBefore == nil {
		return actionResult{title: "Volume comparison source marked", volumeSource: &after, lines: []string{"First Pod: " + pod.Namespace + "/" + pod.PodName, "Close this message, select a Pod volume view, then press x again.", "The second observation will be fetched with current authorisation."}}, nil
	}
	result, err := explain.CompareVolumes(*request.volumeBefore, after)
	if err != nil {
		return actionResult{}, err
	}
	return actionResult{title: "Volume and memory comparison", lines: volumeview.ComparisonLines(*request.volumeBefore, after, result, 100)}, nil
}

func (m *appModel) invalidateActions() {
	next := max(m.action.nextID, m.action.activeID) + 1
	m.action = actionState{nextID: next, activeID: next}
}

func (m *appModel) completeVolumeAction(message actionMsg) {
	request := m.action.pendingRequest
	if request == nil || (request.volumeReader == nil && request.volumeWorkloadReader == nil) {
		return
	}
	if client.IsForbidden(message.err) || client.IsNotFound(message.err) {
		m.clearVolumeTarget()
		m.selectedVolumes.err = message.err
		m.action.volumeCompareSource = nil
	}
	if message.err == nil && message.result.volumeSource != nil {
		m.action.volumeCompareSource = message.result.volumeSource
	}
}
