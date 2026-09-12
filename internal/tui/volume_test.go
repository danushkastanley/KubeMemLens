package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/client"
	"github.com/danushkastanley/kube-memlens/internal/model"
	"github.com/danushkastanley/kube-memlens/internal/volumecontext"
	"github.com/danushkastanley/kube-memlens/internal/volumehealth"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type volumeTUIReader struct {
	*fakeSnapshotReader
	pod                api.PodSnapshot
	volumes            api.PodVolumeContext
	volumeErr          error
	calls              int
	started, cancelled chan struct{}
}

func (r *volumeTUIReader) Pod(context.Context, string, string) (api.PodSnapshot, error) {
	return r.pod, nil
}
func (r *volumeTUIReader) PodVolumes(ctx context.Context, _, _, _ string) (api.PodVolumeContext, error) {
	r.calls++
	if r.started != nil {
		close(r.started)
		<-ctx.Done()
		close(r.cancelled)
		return api.PodVolumeContext{}, ctx.Err()
	}
	return r.volumes, r.volumeErr
}

func volumeTUIFixture(t *testing.T, width, height int) (appModel, *volumeTUIReader) {
	t.Helper()
	now := time.Now().UTC()
	pod := api.PodSnapshot{Namespace: "tenant", PodName: "app", PodUID: "uid", CapturedAt: now, Memory: model.MemoryBreakdown{TotalBytes: 100}, Containers: []api.ContainerSnapshot{{ContainerID: "runtime", ContainerName: "app", CapturedAt: now}}}
	pod.Containers[0].Namespace, pod.Containers[0].PodName, pod.Containers[0].PodUID = pod.Namespace, pod.PodName, pod.PodUID
	view := volumecontext.View{SchemaVersion: 1, Namespace: pod.Namespace, PodName: pod.PodName}
	for _, name := range []string{"data", "scratch", "cache"} {
		view.Volumes = append(view.Volumes, volumecontext.NamedVolume{VolumeName: name, Configuration: volumecontext.Configuration{Kind: volumecontext.EmptyDir, MemoryBacked: true, MountCount: 1}, Usage: volumecontext.SourceState(volumehealth.Unreported, volumecontext.NoReport)})
	}
	r := &volumeTUIReader{fakeSnapshotReader: &fakeSnapshotReader{}, pod: pod, volumes: api.PodVolumeContext{ObjectMeta: metav1.ObjectMeta{Namespace: pod.Namespace, Name: pod.PodName, UID: "uid"}, Context: view}}
	m := newModel(t.Context(), Options{Namespace: "tenant"}, r, "fixture")
	m.width, m.height = width, height
	m.loading = false
	m.data.ContainersLoaded = true
	m.data.Pods = []api.PodSnapshot{pod}
	m.data.Namespaces = []api.NamespaceSnapshot{{Namespace: pod.Namespace, PodCount: 1, Memory: pod.Memory}}
	m.resizeViewports()
	m.detail = entityRef{kind: entityPod, namespace: pod.Namespace, podName: pod.PodName}
	m.detailParent = viewPods
	m.detailSection = detailVolumes
	m.view = viewDetail
	cmd := m.ensureVolumeTarget()
	if cmd == nil {
		t.Fatal("volume query not scheduled")
	}
	m.receiveVolumes(cmd().(volumeMsg))
	return m, r
}

func TestVolumeViewScrollResizeAndIndependentRefresh(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	for _, size := range [][2]int{{80, 24}, {100, 30}, {160, 35}} {
		m, r := volumeTUIFixture(t, size[0], size[1])
		before := r.calls
		var frames strings.Builder
		for index := 0; index < len(m.detailLines(m.width)); index++ {
			frame := m.viewString()
			assertFrameBounds(t, frame, m.width, m.height)
			frames.WriteString(frame)
			updated, _ := m.Update(keyMessage("j"))
			m = updated.(appModel)
		}
		for _, want := range []string{"Volume context", "Volume: data", "Volume: scratch", "no-report", "I/O source:", "Correlation does not prove causality"} {
			if !strings.Contains(frames.String(), want) {
				t.Fatalf("%dx%d missing %q", size[0], size[1], want)
			}
		}
		if r.calls != before || m.volumeRefreshCmd() != nil {
			t.Fatal("scroll/memory cadence refetched volumes")
		}
		updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
		m = updated.(appModel)
		assertFrameBounds(t, m.viewString(), 80, 24)
		m.selectedVolumes.lastAttempt = time.Now().Add(-16 * time.Second)
		if m.volumeRefreshCmd() == nil {
			t.Fatal("15s refresh not scheduled")
		}
		m.cancelVolumeRequest()
	}
}

func TestVolumeRevocationDropsCachedEvidenceAndLateMessages(t *testing.T) {
	m, r := volumeTUIFixture(t, 80, 24)
	if _, ok := m.volumeCommand(); !ok {
		t.Fatal("authorised explicit copy unavailable")
	}
	m.selectedVolumes.lastAttempt = time.Now().Add(-16 * time.Second)
	r.volumeErr = &client.ReadError{Kind: client.ReadErrorForbidden}
	cmd := m.volumeRefreshCmd()
	msg := cmd().(volumeMsg)
	m.receiveVolumes(msg)
	if m.selectedVolumes.context != nil || m.selectedVolumes.previous != nil {
		t.Fatal("revoked evidence retained")
	}
	if _, ok := m.volumeCommand(); ok {
		t.Fatal("revoked command copy allowed")
	}
	if !strings.Contains(strings.Join(m.volumeDetailLines(80), "\n"), "access denied") {
		t.Fatal("denial not visible")
	}
	msg.err = nil
	msg.context = r.volumes
	msg.pod = r.pod
	m.receiveVolumes(msg)
	if m.selectedVolumes.context != nil {
		t.Fatal("late response restored revoked data")
	}
}

func TestVolumeSelectionCancellationAndExpiry(t *testing.T) {
	m, r := volumeTUIFixture(t, 80, 24)
	m.selectedVolumes.lastAttempt = time.Now().Add(-16 * time.Second)
	r.started = make(chan struct{})
	r.cancelled = make(chan struct{})
	cmd := m.volumeRefreshCmd()
	result := make(chan volumeMsg, 1)
	go func() { result <- cmd().(volumeMsg) }()
	select {
	case <-r.started:
	case <-time.After(time.Second):
		t.Fatal("read did not start")
	}
	m.clearVolumeTarget()
	select {
	case <-r.cancelled:
	case <-time.After(time.Second):
		t.Fatal("selection did not cancel read")
	}
	m.receiveVolumes(<-result)
	if m.selectedVolumes.context != nil {
		t.Fatal("cancelled selection restored evidence")
	}
	m, r = volumeTUIFixture(t, 80, 24)
	m.paused = true
	m.selectedVolumes.updatedAt = time.Now().Add(-3 * time.Minute)
	updated, _ := m.Update(tickMsg(time.Now()))
	m = updated.(appModel)
	if m.selectedVolumes.context != nil {
		t.Fatal("paused screen retained expired volume evidence")
	}
}

func TestCompactVolumePaneShowsPauseState(t *testing.T) {
	m, _ := volumeTUIFixture(t, 80, 24)
	m.paused = true
	if !strings.Contains(strings.Join(m.volumeDetailLines(78), "\n"), "Volume refresh paused") {
		t.Fatal("pause state absent from volume pane")
	}
	m.paused = false
	if !strings.Contains(strings.Join(m.volumeDetailLines(78), "\n"), "Volume refresh automatic") {
		t.Fatal("resume state absent from volume pane")
	}
}
