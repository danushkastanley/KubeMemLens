package tui

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/client"
	"github.com/danushkastanley/kube-memlens/internal/model"
)

type fakeSnapshotReader struct {
	current client.CurrentSnapshot
	nodes   []api.NodeSnapshotStatus
	history []api.PodHistory
	err     error
}

func (reader *fakeSnapshotReader) Health(context.Context) error { return reader.err }
func (reader *fakeSnapshotReader) CurrentSnapshot(context.Context) (client.CurrentSnapshot, error) {
	return reader.current, reader.err
}
func (reader *fakeSnapshotReader) Containers(context.Context) ([]api.ContainerSnapshot, error) {
	return reader.current.Containers, reader.err
}
func (reader *fakeSnapshotReader) Pods(context.Context) ([]api.PodSnapshot, error) {
	return reader.current.Pods, reader.err
}
func (reader *fakeSnapshotReader) Namespaces(context.Context) ([]api.NamespaceSnapshot, error) {
	return reader.current.Namespaces, reader.err
}
func (reader *fakeSnapshotReader) Nodes(context.Context) ([]api.NodeSnapshotStatus, error) {
	return reader.nodes, reader.err
}
func (reader *fakeSnapshotReader) Workloads(context.Context) ([]api.WorkloadSnapshot, error) {
	return reader.current.Workloads, reader.err
}
func (reader *fakeSnapshotReader) PodHistory(context.Context, string, string) ([]api.PodHistory, error) {
	return reader.history, reader.err
}
func (reader *fakeSnapshotReader) DebugStore(context.Context) (api.DebugStore, error) {
	return api.DebugStore{}, reader.err
}

type basicSnapshotReader struct{ inner *fakeSnapshotReader }

func (reader basicSnapshotReader) Health(ctx context.Context) error { return reader.inner.Health(ctx) }
func (reader basicSnapshotReader) Containers(ctx context.Context) ([]api.ContainerSnapshot, error) {
	return reader.inner.Containers(ctx)
}
func (reader basicSnapshotReader) Pods(ctx context.Context) ([]api.PodSnapshot, error) {
	return reader.inner.Pods(ctx)
}
func (reader basicSnapshotReader) Namespaces(ctx context.Context) ([]api.NamespaceSnapshot, error) {
	return reader.inner.Namespaces(ctx)
}
func (reader basicSnapshotReader) Nodes(ctx context.Context) ([]api.NodeSnapshotStatus, error) {
	return reader.inner.Nodes(ctx)
}
func (reader basicSnapshotReader) Workloads(ctx context.Context) ([]api.WorkloadSnapshot, error) {
	return reader.inner.Workloads(ctx)
}
func (reader basicSnapshotReader) PodHistory(ctx context.Context, namespace, pod string) ([]api.PodHistory, error) {
	return reader.inner.PodHistory(ctx, namespace, pod)
}
func (reader basicSnapshotReader) DebugStore(ctx context.Context) (api.DebugStore, error) {
	return reader.inner.DebugStore(ctx)
}

func TestFetchAdaptersProduceCompleteSnapshotData(t *testing.T) {
	reader := tuiFixtureReader()
	for name, adapter := range map[string]client.SnapshotReader{
		"current pages": reader,
		"legacy lists":  basicSnapshotReader{inner: reader},
	} {
		t.Run(name, func(t *testing.T) {
			m := newModel(context.Background(), Options{}, adapter, "test")
			message := m.fetchCmd()().(fetchMsg)
			if message.err != nil {
				t.Fatalf("fetch: %v", message.err)
			}
			if len(message.data.Nodes) != 1 || len(message.data.Pods) != 2 || len(message.data.Containers) != 2 || len(message.data.Workloads) != 1 {
				t.Fatalf("snapshot data = %#v", message.data)
			}
		})
	}
}

func TestFetchAndHistoryFailuresRemainExplicit(t *testing.T) {
	reader := tuiFixtureReader()
	reader.err = errors.New("collector unavailable")
	m := newModel(context.Background(), Options{}, reader, "test")
	if message := m.fetchCmd()().(fetchMsg); message.err == nil {
		t.Fatal("fetch error was swallowed")
	}
	m.selectedHistory.selectPod("default", "api-0")
	request, _ := m.selectedHistory.start()
	if message := m.fetchHistoryCmd(request)().(historyMsg); message.err == nil {
		t.Fatal("history error was swallowed")
	}
}

func TestAllEntityFramesAndDetailsRender(t *testing.T) {
	m := loadedFixtureModel(t, 120, 35)
	frames := map[viewMode][]string{
		viewNodes:      {"NODE", "POD CHARGE", "node-a"},
		viewNamespaces: {"NAMESPACE", "default"},
		viewWorkloads:  {"WORKLOAD", "Deployment", "api"},
		viewPods:       {"A/F/S/O", "api-0", "LIMIT"},
		viewContainers: {"CONTAINER", "app"},
	}
	for view, expected := range frames {
		m.view = view
		m.resizeViewports()
		m.reconcileCurrentViewport("")
		frame := m.viewString()
		for _, want := range expected {
			if !strings.Contains(frame, want) {
				t.Fatalf("view %v missing %q:\n%s", view, want, frame)
			}
		}
	}

	refs := []struct {
		ref  entityRef
		want string
	}{
		{ref: entityRef{kind: entityNode, nodeName: "node-a"}, want: "Summed Pod charge"},
		{ref: entityRef{kind: entityNamespace, namespace: "default"}, want: "Namespace: default"},
		{ref: entityRef{kind: entityWorkload, namespace: "default", workloadKind: "Deployment", name: "api"}, want: "Replicas observed"},
		{ref: entityRef{kind: entityPod, namespace: "default", podName: "api-0"}, want: "Memory composition"},
		{ref: entityRef{kind: entityContainer, namespace: "default", podName: "api-0", containerName: "app"}, want: "Container: app"},
	}
	for _, test := range refs {
		m.detail = test.ref
		m.view = viewDetail
		if joined := strings.Join(m.detailLines(100), "\n"); !strings.Contains(joined, test.want) {
			t.Fatalf("detail %#v missing %q:\n%s", test.ref, test.want, joined)
		}
	}
}

func TestHeaderHidesConnectionImplementationDetails(t *testing.T) {
	m := loadedFixtureModel(t, 160, 35)
	header := m.renderHeader(160)
	if strings.Contains(header, "connection:") || strings.Contains(header, m.connectionDescription) {
		t.Fatalf("header exposes connection implementation details: %q", header)
	}
}

func TestSuccessfulAutomaticRefreshKeepsHeaderStable(t *testing.T) {
	m := loadedFixtureModel(t, 160, 35)
	m.lastRefresh = time.Now().Add(-time.Second)
	before := m.renderHeader(160)

	updated, _ := m.Update(tickMsg(time.Now()))
	m = updated.(appModel)
	during := m.renderHeader(160)

	updated, _ = m.Update(fetchMsg{generation: m.fetchGeneration, data: m.data})
	m = updated.(appModel)
	after := m.renderHeader(160)

	if before != during || during != after {
		t.Fatalf("header changed during successful automatic refresh:\nbefore: %q\nduring: %q\nafter:  %q", before, during, after)
	}
}

func TestPodViewsUseKubernetesCreationAge(t *testing.T) {
	reader := tuiFixtureReader()
	createdAt := time.Now().UTC().Add(-49 * time.Hour)
	for index := range reader.current.Pods {
		reader.current.Pods[index].Context.CreatedAt = createdAt
	}

	m := newModel(context.Background(), Options{AllNamespaces: true}, reader, "test")
	m.width, m.height = 160, 35
	m.resizeViewports()
	message := m.fetchCmd()().(fetchMsg)
	updated, _ := m.Update(message)
	m = updated.(appModel)
	m.loading = false
	m.view = viewPods

	if frame := m.renderPods(160); !strings.Contains(frame, "2d") {
		t.Fatalf("Pod table does not show Kubernetes creation age:\n%s", frame)
	}
	if detail := strings.Join(compactPodLines(reader.current.Pods[0]), "\n"); !strings.Contains(detail, "Pod age") || !strings.Contains(detail, "2d") {
		t.Fatalf("Pod detail does not show Kubernetes creation age:\n%s", detail)
	}
}

func TestLoadingEmptyErrorHelpAndActionFrames(t *testing.T) {
	m := newModel(context.Background(), Options{}, tuiFixtureReader(), "test")
	m.width, m.height = 80, 24
	if frame := m.viewString(); !strings.Contains(frame, "Loading collector snapshots") {
		t.Fatalf("loading frame:\n%s", frame)
	}
	m.loading = false
	if frame := m.viewString(); !strings.Contains(frame, "No collector snapshots") {
		t.Fatalf("empty frame:\n%s", frame)
	}
	m.statusErr = errors.New("offline")
	if frame := m.viewString(); !strings.Contains(frame, "Could not connect") {
		t.Fatalf("error frame:\n%s", frame)
	}
	m.statusErr = nil
	m.help = true
	if frame := m.viewString(); !strings.Contains(frame, "Keybindings") || !strings.Contains(frame, "incident action menu") {
		t.Fatalf("help frame:\n%s", frame)
	}
	m.help = false
	m.action.mode = actionMenu
	if frame := m.viewString(); !strings.Contains(frame, "Incident actions") || !strings.Contains(frame, "No action mutates") {
		t.Fatalf("action frame:\n%s", frame)
	}
}

func TestKeyboardStateMachineCoversViewsSearchPauseSortAndDetail(t *testing.T) {
	m := loadedFixtureModel(t, 120, 35)
	press := func(key string) {
		updated, _ := m.handleKey(keyMessage(key))
		m = updated.(appModel)
	}
	press("N")
	if m.view != viewNodes {
		t.Fatalf("N view = %v", m.view)
	}
	press("n")
	press("w")
	press("p")
	press("c")
	if m.view != viewContainers {
		t.Fatalf("c view = %v", m.view)
	}
	press("/")
	press("a")
	press("p")
	press("p")
	press("backspace")
	press("enter")
	if m.searching || m.query != "ap" {
		t.Fatalf("search state searching=%t query=%q", m.searching, m.query)
	}
	press("esc")
	press("p")
	press("down")
	press("up")
	press("pgdown")
	press("pgup")
	press("G")
	press("g")
	beforeSort := m.sort
	press("s")
	if m.sort == beforeSort {
		t.Fatal("sort did not advance")
	}
	press(" ")
	if !m.paused {
		t.Fatal("pause did not toggle")
	}
	press("enter")
	if m.view != viewDetail {
		t.Fatalf("enter view = %v", m.view)
	}
	press("h")
	if m.view != viewPods {
		t.Fatalf("back view = %v", m.view)
	}
}

func TestActionStateMachineRecommendationCompareCaptureAndCopy(t *testing.T) {
	m := loadedFixtureModel(t, 120, 35)
	m.view = viewPods
	m.reconcileCurrentViewport("")

	updated, command := m.handleKey(keyMessage("R"))
	m = updated.(appModel)
	if command == nil || !m.action.inFlight {
		t.Fatal("recommendation action did not start")
	}
	m = applyCommand(t, m, command)
	if !strings.Contains(m.viewString(), "Automatic mutation: disabled") {
		t.Fatalf("recommendation result:\n%s", m.viewString())
	}
	updated, _ = m.handleActionKey(keyMessage("esc"))
	m = updated.(appModel)

	updated, command = m.handleKey(keyMessage("x"))
	m = updated.(appModel)
	if command != nil || m.action.compareSource == nil {
		t.Fatal("comparison source was not marked")
	}
	updated, _ = m.handleActionKey(keyMessage("esc"))
	m = updated.(appModel)
	m.currentViewport().move(1)
	updated, command = m.handleKey(keyMessage("x"))
	m = updated.(appModel)
	m = applyCommand(t, m, command)
	if !strings.Contains(m.viewString(), "Live Pod comparison") {
		t.Fatalf("comparison result:\n%s", m.viewString())
	}
	updated, _ = m.handleActionKey(keyMessage("esc"))
	m = updated.(appModel)

	path := filepath.Join(t.TempDir(), "incident.json")
	m.action.mode = actionCapturePath
	m.action.input = path
	updated, command = m.handleActionKey(keyMessage("enter"))
	m = updated.(appModel)
	m = applyCommand(t, m, command)
	if !strings.Contains(m.viewString(), "Redacted capture written") {
		t.Fatalf("capture result:\n%s", m.viewString())
	}
	updated, _ = m.handleActionKey(keyMessage("esc"))
	m = updated.(appModel)

	updated, command = m.handleKey(keyMessage("y"))
	m = updated.(appModel)
	if command == nil || !strings.Contains(m.viewString(), "Command copied") {
		t.Fatalf("copy result:\n%s", m.viewString())
	}
}

func TestRenderedFramesExcludeRuntimeIdentifiers(t *testing.T) {
	m := loadedFixtureModel(t, 180, 50)
	for _, view := range []viewMode{viewNodes, viewNamespaces, viewWorkloads, viewPods, viewContainers} {
		m.view = view
		frame := m.viewString()
		for _, forbidden := range []string{"sensitive-uid", "container-secret", "/sensitive/cgroup", "customer-secret"} {
			if strings.Contains(frame, forbidden) {
				t.Fatalf("view %v contains %q", view, forbidden)
			}
		}
	}
}

func applyCommand(t *testing.T, m appModel, command tea.Cmd) appModel {
	t.Helper()
	if command == nil {
		t.Fatal("expected command")
	}
	updated, _ := m.Update(command())
	return updated.(appModel)
}

func keyMessage(key string) tea.KeyPressMsg {
	switch key {
	case "enter":
		return tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter})
	case "esc":
		return tea.KeyPressMsg(tea.Key{Code: tea.KeyEsc})
	case "backspace":
		return tea.KeyPressMsg(tea.Key{Code: tea.KeyBackspace})
	case "up":
		return tea.KeyPressMsg(tea.Key{Code: tea.KeyUp})
	case "down":
		return tea.KeyPressMsg(tea.Key{Code: tea.KeyDown})
	case "pgup":
		return tea.KeyPressMsg(tea.Key{Code: tea.KeyPgUp})
	case "pgdown":
		return tea.KeyPressMsg(tea.Key{Code: tea.KeyPgDown})
	case " ":
		return tea.KeyPressMsg(tea.Key{Code: tea.KeySpace, Text: " "})
	default:
		runes := []rune(key)
		return tea.KeyPressMsg(tea.Key{Code: runes[0], Text: key})
	}
}

func loadedFixtureModel(t *testing.T, width, height int) appModel {
	t.Helper()
	reader := tuiFixtureReader()
	m := newModel(context.Background(), Options{AllNamespaces: true}, reader, "test")
	m.width, m.height = width, height
	m.resizeViewports()
	message := m.fetchCmd()().(fetchMsg)
	updated, _ := m.Update(message)
	m = updated.(appModel)
	m.loading = false
	return m
}

func tuiFixtureReader() *fakeSnapshotReader {
	now := time.Now().UTC()
	containerA := api.ContainerSnapshot{
		Namespace: "default", PodName: "api-0", PodUID: "sensitive-uid", ContainerName: "app", ContainerID: "container-secret", NodeName: "node-a", CgroupPath: "/sensitive/cgroup", CapturedAt: now,
		DeltaStartedAt: now.Add(-5 * time.Second), DeltaWindowKnown: true,
		Context: api.ContainerContext{MemoryRequestKnown: true, MemoryRequestBytes: 64 << 20, MemoryLimitKnown: true, MemoryLimitBytes: 256 << 20, QoSClass: "Burstable", WorkloadKind: "Deployment", WorkloadName: "api", Labels: map[string]string{"customer": "customer-secret"}},
		Memory:  model.MemoryBreakdown{TotalBytes: 128 << 20, AnonBytes: 80 << 20, FileBytes: 32 << 20, MaxKnown: true, MaxBytes: 256 << 20, EventDeltasKnown: true},
	}
	containerB := containerA
	containerB.PodName = "api-1"
	containerB.PodUID = "second-sensitive-uid"
	containerB.ContainerID = "second-container-secret"
	containerB.Memory = model.MemoryBreakdown{TotalBytes: 192 << 20, AnonBytes: 128 << 20, FileBytes: 48 << 20, MaxKnown: true, MaxBytes: 256 << 20, EventDeltasKnown: true, HighEventsDelta: 1, PressureKnown: true, PSISomeAvg10: 2}
	pods := []api.PodSnapshot{
		{Namespace: "default", PodName: "api-0", PodUID: containerA.PodUID, NodeName: "node-a", CapturedAt: now, Containers: []api.ContainerSnapshot{containerA}, Context: api.PodContext{WorkloadKind: "Deployment", WorkloadName: "api", MemoryLimitBytes: 256 << 20, MemoryLimitContainers: 1}, Memory: containerA.Memory},
		{Namespace: "default", PodName: "api-1", PodUID: containerB.PodUID, NodeName: "node-a", CapturedAt: now, Containers: []api.ContainerSnapshot{containerB}, Context: api.PodContext{WorkloadKind: "Deployment", WorkloadName: "api", MemoryLimitBytes: 256 << 20, MemoryLimitContainers: 1}, Memory: containerB.Memory},
	}
	workload := api.WorkloadSnapshot{Namespace: "default", Kind: "Deployment", Name: "api", CapturedAt: now, PodCount: 2, LargestPodName: "api-1", LargestPodBytes: containerB.Memory.TotalBytes, Pods: pods, Memory: model.AddMemory(containerA.Memory, containerB.Memory)}
	return &fakeSnapshotReader{
		current: client.CurrentSnapshot{
			Containers: []api.ContainerSnapshot{containerA, containerB}, Pods: pods,
			Namespaces: []api.NamespaceSnapshot{{Namespace: "default", CapturedAt: now, PodCount: 2, Memory: workload.Memory}},
			Workloads:  []api.WorkloadSnapshot{workload},
		},
		nodes:   []api.NodeSnapshotStatus{{NodeName: "node-a", CapturedAt: now, ContainerCount: 2, Environment: api.NodeEnvironment{NodeContextAvailable: true, MemoryPressureStatus: "False", MemoryAllocatableKnown: true, MemoryAllocatableBytes: 8 << 30, CgroupVersion: "v2", CgroupDriver: "systemd", ContainerRuntimes: []string{"containerd"}}}},
		history: []api.PodHistory{{Namespace: "default", PodName: "api-0", PodUID: containerA.PodUID, NodeName: "node-a", Points: []api.MemoryHistoryPoint{{CapturedAt: now.Add(-5 * time.Second), TotalBytes: 100 << 20}, {CapturedAt: now, TotalBytes: 128 << 20}}}},
	}
}
