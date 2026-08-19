package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/model"
)

func TestNewModelDefaultsToPods(t *testing.T) {
	m := newModel(context.Background(), Options{}, nil, "test")
	if m.view != viewPods || m.sort != sortRisk {
		t.Fatalf("default view=%v sort=%v", m.view, m.sort)
	}
}

func TestNodeViewCombinesStatusAndObservedPodCharge(t *testing.T) {
	now := time.Now()
	nodes := []api.NodeSnapshotStatus{{
		NodeName: "node-a", CapturedAt: now, ContainerCount: 3,
		Environment: api.NodeEnvironment{NodeContextAvailable: true, MemoryPressureStatus: "False", MemoryAllocatableKnown: true, MemoryAllocatableBytes: 8 << 30},
	}}
	pods := []api.PodSnapshot{
		{NodeName: "node-a", Memory: model.MemoryBreakdown{TotalBytes: 100}},
		{NodeName: "node-a", Memory: model.MemoryBreakdown{TotalBytes: 200}},
	}
	views := buildNodeViews(nodes, pods, "")
	if len(views) != 1 || views[0].podCount != 2 || views[0].containerCount != 3 || views[0].memory.TotalBytes != 300 {
		t.Fatalf("node views = %#v", views)
	}
}

func TestContainerOpensFirstClassContainerDetail(t *testing.T) {
	container := api.ContainerSnapshot{
		Namespace: "default", PodName: "api", ContainerName: "sidecar", NodeName: "node-a",
		CapturedAt: time.Now(), Memory: model.MemoryBreakdown{TotalBytes: 100, AnonBytes: 50},
	}
	m := newModel(context.Background(), Options{AllNamespaces: true}, nil, "test")
	m.view = viewContainers
	m.data.Containers = []api.ContainerSnapshot{container}
	m.data.Pods = []api.PodSnapshot{{Namespace: "default", PodName: "api", NodeName: "node-a", Containers: []api.ContainerSnapshot{container}, Memory: container.Memory}}
	m.reconcileCurrentViewport("")
	cmd := m.openSelectedDetail()
	if cmd == nil || m.detail.kind != entityContainer || m.detail.containerName != "sidecar" {
		t.Fatalf("container detail = %#v", m.detail)
	}
	joined := strings.Join(m.detailLines(100), "\n")
	if !strings.Contains(joined, "Container: sidecar") || !strings.Contains(joined, "Parent-Pod history") {
		t.Fatalf("container detail lines:\n%s", joined)
	}
}

func TestNodeDrillFiltersPodsAndBackRestoresNodeView(t *testing.T) {
	m := newModel(context.Background(), Options{AllNamespaces: true}, nil, "test")
	m.view = viewNodes
	m.data.Nodes = []api.NodeSnapshotStatus{{NodeName: "node-a"}, {NodeName: "node-b"}}
	m.data.Pods = []api.PodSnapshot{{Namespace: "default", PodName: "a", NodeName: "node-a"}, {Namespace: "default", PodName: "b", NodeName: "node-b"}}
	m.reconcileCurrentViewport("")
	m.enter()
	if m.view != viewPods || len(m.visiblePods()) != 1 || m.visiblePods()[0].NodeName != m.currentNode {
		t.Fatalf("node drill view=%v node=%q pods=%#v", m.view, m.currentNode, m.visiblePods())
	}
	m.back()
	if m.view != viewNodes || m.currentNode != "" {
		t.Fatalf("back view=%v node=%q", m.view, m.currentNode)
	}
}

func TestNodeToPodToContainerJourneyKeepsScope(t *testing.T) {
	m := newModel(context.Background(), Options{AllNamespaces: true}, nil, "test")
	m.view = viewNodes
	m.data.Nodes = []api.NodeSnapshotStatus{{NodeName: "node-a"}, {NodeName: "node-b"}}
	m.data.Pods = []api.PodSnapshot{{Namespace: "default", PodName: "api", NodeName: "node-a"}}
	m.data.Containers = []api.ContainerSnapshot{
		{Namespace: "default", PodName: "api", ContainerName: "app", NodeName: "node-a"},
		{Namespace: "default", PodName: "other", ContainerName: "app", NodeName: "node-b"},
	}
	m.reconcileCurrentViewport("")
	m.enter()
	m.view = viewContainers
	m.resetCurrentViewport()
	if items := m.visibleContainers(); len(items) != 1 || items[0].NodeName != "node-a" {
		t.Fatalf("node-scoped containers = %#v", items)
	}
	m.enter()
	if m.view != viewDetail || m.detail.kind != entityContainer {
		t.Fatalf("container drill view=%v detail=%#v", m.view, m.detail)
	}
	m.back()
	m.back()
	if m.view != viewPods || m.currentNode != "node-a" {
		t.Fatalf("container back did not restore scoped Pods: view=%v node=%q", m.view, m.currentNode)
	}
}

func TestWorkloadToPodToContainerJourneyKeepsScope(t *testing.T) {
	m := newModel(context.Background(), Options{AllNamespaces: true}, nil, "test")
	m.currentNamespace = "default"
	m.currentWorkloadKind = "Deployment"
	m.currentWorkloadName = "api"
	m.view = viewContainers
	m.data.Containers = []api.ContainerSnapshot{
		{Namespace: "default", PodName: "api-0", ContainerName: "app", Context: api.ContainerContext{WorkloadKind: "Deployment", WorkloadName: "api"}},
		{Namespace: "default", PodName: "worker-0", ContainerName: "app", Context: api.ContainerContext{WorkloadKind: "Deployment", WorkloadName: "worker"}},
	}
	items := m.visibleContainers()
	if len(items) != 1 || items[0].PodName != "api-0" {
		t.Fatalf("workload-scoped containers = %#v", items)
	}
}
