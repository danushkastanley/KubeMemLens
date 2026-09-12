package tui

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/client"
	"github.com/danushkastanley/kube-memlens/internal/volumecontext"
)

const volumeRefreshInterval = 15 * time.Second

type detailSection uint8

const (
	detailMemory detailSection = iota
	detailVolumes
)

type selectedVolumes struct {
	namespace, name, uid, kind string
	generation                 uint64
	inFlight                   bool
	lastAttempt, updatedAt     time.Time
	pod                        api.PodSnapshot
	previous                   *api.PodSnapshot
	context                    *api.PodVolumeContext
	workload                   *api.WorkloadVolumeContext
	err                        error
}

type volumeRequest struct {
	namespace, name, uid, kind string
	generation                 uint64
}

type volumeMsg struct {
	request  volumeRequest
	pod      api.PodSnapshot
	context  api.PodVolumeContext
	workload *api.WorkloadVolumeContext
	err      error
}

func (m appModel) volumeTarget() (volumeRequest, bool) {
	if m.restricted() {
		return volumeRequest{}, false
	}
	if m.view == viewDetail && m.detail.kind == entityWorkload {
		return volumeRequest{namespace: m.detail.namespace, name: m.detail.name, kind: m.detail.workloadKind}, true
	}
	if m.view == viewWorkloads && m.layout().splitDetail {
		if ref, ok := m.currentEntityRef(); ok {
			return volumeRequest{namespace: ref.namespace, name: ref.name, kind: ref.workloadKind}, true
		}
	}
	var pod api.PodSnapshot
	var ok bool
	if m.view == viewDetail && (m.detail.kind == entityPod || m.detail.kind == entityContainer) {
		pod, ok = m.findPod(m.detail.namespace, m.detail.podName)
	}
	if m.view == viewPods && m.layout().splitDetail {
		pod, ok = m.selectedVisiblePod()
	}
	return volumeRequest{namespace: pod.Namespace, name: pod.PodName, uid: pod.PodUID}, ok && pod.PodUID != ""
}

func (m *appModel) ensureVolumeTarget() tea.Cmd {
	target, ok := m.volumeTarget()
	if !ok {
		m.clearVolumeTarget()
		return nil
	}
	s := &m.selectedVolumes
	if s.namespace != target.namespace || s.name != target.name || s.uid != target.uid || s.kind != target.kind {
		m.clearVolumeTarget()
		s.namespace, s.name, s.uid, s.kind = target.namespace, target.name, target.uid, target.kind
	}
	return m.volumeRefreshCmd()
}

func (m *appModel) volumeRefreshCmd() tea.Cmd {
	s := &m.selectedVolumes
	if m.paused || s.name == "" || s.inFlight || time.Since(s.lastAttempt) < volumeRefreshInterval {
		return nil
	}
	reader, podOK := m.client.(client.PodVolumeReader)
	workloadReader, workloadOK := m.client.(client.WorkloadVolumeReader)
	if (s.kind == "" && !podOK) || (s.kind != "" && !workloadOK) {
		return nil
	}
	s.generation++
	s.inFlight = true
	s.lastAttempt = time.Now().UTC()
	request := volumeRequest{namespace: s.namespace, name: s.name, uid: s.uid, kind: s.kind, generation: s.generation}
	ctx, cancel := context.WithCancel(m.ctx)
	m.volumeCancel = cancel
	return func() tea.Msg {
		defer cancel()
		if request.kind != "" {
			value, err := workloadReader.WorkloadVolumes(ctx, request.namespace, request.kind, request.name)
			return volumeMsg{request: request, workload: &value, err: err}
		}
		pod, volumes, err := client.ReadPodVolumeEvidence(ctx, reader, request.namespace, request.name, request.uid)
		return volumeMsg{request: request, pod: pod, context: volumes, err: err}
	}
}

func (m *appModel) cancelVolumeRequest() {
	if m.volumeCancel != nil {
		m.volumeCancel()
		m.volumeCancel = nil
	}
	m.selectedVolumes.generation++
	m.selectedVolumes.inFlight = false
}

func (m *appModel) clearVolumeTarget() {
	m.cancelVolumeRequest()
	m.selectedVolumes = selectedVolumes{generation: m.selectedVolumes.generation}
}

func (m *appModel) expireVolumes(now time.Time) {
	if before := m.action.volumeCompareSource; before != nil && now.Sub(before.Now) > volumecontext.ExpireAfter {
		m.action.volumeCompareSource = nil
	}
	s := &m.selectedVolumes
	if (s.context != nil || s.workload != nil) && now.Sub(s.updatedAt) > volumecontext.ExpireAfter {
		s.context, s.previous, s.pod = nil, nil, api.PodSnapshot{}
		s.workload = nil
		m.invalidateActions()
	}
}

func (m *appModel) receiveVolumes(msg volumeMsg) {
	s := &m.selectedVolumes
	if !s.inFlight || msg.request != (volumeRequest{namespace: s.namespace, name: s.name, uid: s.uid, kind: s.kind, generation: s.generation}) {
		return
	}
	if m.volumeCancel != nil {
		m.volumeCancel()
		m.volumeCancel = nil
	}
	s.inFlight, s.err = false, msg.err
	if msg.err == nil {
		if msg.workload != nil {
			if s.workload != nil && s.workload.UID != msg.workload.UID {
				m.invalidateActions()
			}
			s.workload = msg.workload
			s.context, s.previous, s.pod = nil, nil, api.PodSnapshot{}
		} else {
			if s.context != nil {
				before := s.pod
				s.previous = &before
			}
			s.pod, s.context = msg.pod, &msg.context
		}
		s.updatedAt = time.Now().UTC()
	} else if client.IsForbidden(msg.err) || client.IsNotFound(msg.err) {
		s.context, s.previous, s.pod = nil, nil, api.PodSnapshot{}
		s.workload = nil
		s.updatedAt = time.Time{}
		m.invalidateActions()
	}
	if m.view == viewDetail {
		m.syncDetailViewport()
	} else if m.layout().splitDetail {
		m.syncInlineDetailViewport()
	}
}
