package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/model"
)

func TestLongPodTableScrollsSelectionIntoView(t *testing.T) {
	m := newModel(context.Background(), Options{AllNamespaces: true}, nil, "test")
	m.view = viewPods
	m.width, m.height = 100, 24
	m.loading = false
	m.data.Namespaces = []api.NamespaceSnapshot{{Namespace: "default"}}
	m.resizeViewports()
	for index := 0; index < 100; index++ {
		m.data.Pods = append(m.data.Pods, api.PodSnapshot{
			Namespace: "default",
			PodName:   fmt.Sprintf("pod-%03d", index),
			Memory:    model.MemoryBreakdown{TotalBytes: uint64(100 - index)},
		})
	}
	m.reconcileCurrentViewport("")
	m.currentViewport().last()

	frame := m.View()
	if !strings.Contains(frame, "›") || !strings.Contains(frame, "pod-099") {
		t.Fatalf("last selected Pod is not visible:\n%s", frame)
	}
	if strings.Contains(frame, "pod-000") {
		t.Fatalf("rendered the first page instead of the selected window:\n%s", frame)
	}
}

func TestDetailCanReachFinalCommands(t *testing.T) {
	pod := api.PodSnapshot{
		Namespace: "default",
		PodName:   "api",
		NodeName:  "node-a",
		Memory:    model.MemoryBreakdown{TotalBytes: 1 << 30, AnonBytes: 1 << 29},
	}
	m := newModel(context.Background(), Options{AllNamespaces: true}, nil, "test")
	m.width, m.height = 80, 24
	m.loading = false
	m.data.Namespaces = []api.NamespaceSnapshot{{Namespace: "default"}}
	m.data.Pods = []api.PodSnapshot{pod}
	m.selectedPodNS, m.selectedPodName = pod.Namespace, pod.PodName
	m.view = viewDetail
	m.resizeViewports()
	m.syncDetailViewport()
	m.currentViewport().last()

	frame := m.View()
	if !strings.Contains(frame, "kubectl describe pod/api -n default") {
		t.Fatalf("final detail command is unreachable:\n%s", frame)
	}
}

func TestRefreshKeepsStablePodSelection(t *testing.T) {
	m := newModel(context.Background(), Options{AllNamespaces: true}, nil, "test")
	m.view = viewPods
	m.data.Pods = []api.PodSnapshot{
		{Namespace: "default", PodName: "first", Memory: model.MemoryBreakdown{TotalBytes: 20}},
		{Namespace: "default", PodName: "selected", Memory: model.MemoryBreakdown{TotalBytes: 10}},
	}
	m.reconcileCurrentViewport("")
	m.currentViewport().move(1)

	updated, _ := m.Update(fetchMsg{data: snapshotData{Pods: []api.PodSnapshot{
		{Namespace: "default", PodName: "selected", Memory: model.MemoryBreakdown{TotalBytes: 30}},
		{Namespace: "default", PodName: "first", Memory: model.MemoryBreakdown{TotalBytes: 5}},
	}}})
	m = updated.(appModel)
	if got := m.selectedEntityKey(); got != "pod/default/selected" {
		t.Fatalf("selected key after refresh = %q", got)
	}
}

func TestPageMovementUsesCurrentCapacity(t *testing.T) {
	m := newModel(context.Background(), Options{AllNamespaces: true}, nil, "test")
	m.view = viewPods
	m.width, m.height = 100, 24
	m.resizeViewports()
	for index := 0; index < 100; index++ {
		m.data.Pods = append(m.data.Pods, api.PodSnapshot{Namespace: "default", PodName: fmt.Sprintf("pod-%03d", index)})
	}
	m.reconcileCurrentViewport("")

	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyPgDown})
	m = updated.(appModel)
	if m.currentViewport().selected != m.currentViewport().capacity {
		t.Fatalf("page down selected %d, capacity %d", m.currentViewport().selected, m.currentViewport().capacity)
	}
}
