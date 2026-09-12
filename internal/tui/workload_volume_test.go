package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/aggregate"
	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/client"
	"github.com/danushkastanley/kube-memlens/internal/volumecontext"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type workloadTUIReader struct {
	*volumeTUIReader
	value         api.WorkloadVolumeContext
	err           error
	workloadCalls int
}

func (r *workloadTUIReader) WorkloadVolumes(context.Context, string, string, string) (api.WorkloadVolumeContext, error) {
	r.workloadCalls++
	return r.value, r.err
}

func workloadTUIFixture(t *testing.T, width, height int) (appModel, *workloadTUIReader) {
	t.Helper()
	m, podReader := volumeTUIFixture(t, width, height)
	pod := podReader.pod
	pod.Containers = append([]api.ContainerSnapshot(nil), pod.Containers...)
	for i := range pod.Containers {
		pod.Containers[i].Namespace = pod.Namespace
		pod.Containers[i].PodName = pod.PodName
		pod.Containers[i].PodUID = pod.PodUID
	}
	volume := podReader.volumes
	volume.TypeMeta = metav1.TypeMeta{APIVersion: api.MemoryAPIGroup + "/" + api.MemoryAPIVersion, Kind: "PodVolumeContext"}
	groups, err := volumecontext.GroupWorkload([]volumecontext.View{volume.Context}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	value := api.WorkloadVolumeContext{TypeMeta: metav1.TypeMeta{APIVersion: api.MemoryAPIGroup + "/" + api.MemoryAPIVersion, Kind: "WorkloadVolumeContext"}, ObjectMeta: metav1.ObjectMeta{Namespace: "tenant", Name: "workload", UID: "workload-uid"}, ObservedAt: time.Now().UTC(), Workload: aggregate.SummariseWorkload("tenant", "Deployment", "workload", []api.PodSnapshot{pod}), PodVolumes: []api.PodVolumeContext{volume}, Filesystems: groups}
	if err := api.ValidateWorkloadVolumeContext(value, time.Now()); err != nil {
		t.Fatal(err)
	}
	r := &workloadTUIReader{volumeTUIReader: podReader, value: value}
	m.client = r
	m.data.Workloads = []api.WorkloadSnapshot{value.Workload}
	m.view = viewWorkloads
	m.detailSection = detailMemory
	m.resetCurrentViewport()
	cmd := m.openVolumeDetail()
	if cmd == nil {
		t.Fatal("workload volume read not scheduled")
	}
	m.receiveVolumes(cmd().(volumeMsg))
	return m, r
}

func TestWorkloadVolumeViewNavigationAndRevocation(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	for _, size := range [][2]int{{80, 24}, {160, 35}} {
		m, r := workloadTUIFixture(t, size[0], size[1])
		if m.detail.kind != entityWorkload || m.detailSection != detailVolumes || r.workloadCalls != 1 {
			t.Fatal("workload context did not open")
		}
		var frames strings.Builder
		for i := 0; i < len(m.detailLines(m.width)); i++ {
			frame := m.viewString()
			assertFrameBounds(t, frame, m.width, m.height)
			frames.WriteString(frame)
			m.move(1)
		}
		for _, want := range []string{"Workload volumes: Deployment/tenant/workload", "Filesystem group 1", "Pod evidence: app", "values are not summed"} {
			if !strings.Contains(frames.String(), want) {
				t.Fatal("missing workload field", want)
			}
		}
		if command, ok := m.volumeCommand(); !ok || !strings.Contains(command, "volumes workload 'Deployment/workload'") {
			t.Fatal("workload follow-up command missing")
		}
		r.err = &client.ReadError{Kind: client.ReadErrorForbidden}
		m.selectedVolumes.lastAttempt = time.Now().Add(-16 * time.Second)
		cmd := m.volumeRefreshCmd()
		m.receiveVolumes(cmd().(volumeMsg))
		if m.selectedVolumes.workload != nil {
			t.Fatal("revocation retained workload data")
		}
		if _, ok := m.volumeCommand(); ok {
			t.Fatal("revoked workload command remained available")
		}
		m.back()
		if m.view != viewWorkloads {
			t.Fatal("back lost workload parent")
		}
	}
}
