package tui

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/client"
	"github.com/danushkastanley/kube-memlens/internal/model"
)

type cancellableHistoryReader struct {
	*fakeSnapshotReader
	started   chan string
	cancelled chan string
}

func (reader *cancellableHistoryReader) PodHistory(ctx context.Context, _, podName string) ([]api.PodHistory, error) {
	reader.started <- podName
	<-ctx.Done()
	reader.cancelled <- podName
	return nil, ctx.Err()
}

func TestSelectedHistoryRejectsLateResponseForPreviousPod(t *testing.T) {
	var state selectedHistory
	state.selectPod("default", "pod-a")
	requestA, ok := state.start()
	if !ok {
		t.Fatal("expected request A")
	}
	state.selectPod("default", "pod-b")
	requestB, ok := state.start()
	if !ok {
		t.Fatal("expected request B")
	}

	if state.complete(historyMsg{namespace: requestA.namespace, podName: requestA.podName, generation: requestA.generation}, time.Now()) {
		t.Fatal("late response for Pod A was accepted")
	}
	want := []api.PodHistory{{Namespace: "default", PodName: "pod-b"}}
	if !state.complete(historyMsg{namespace: requestB.namespace, podName: requestB.podName, generation: requestB.generation, series: want}, time.Now()) {
		t.Fatal("current response for Pod B was rejected")
	}
	if len(state.series) != 1 || state.series[0].PodName != "pod-b" {
		t.Fatalf("history = %#v", state.series)
	}
}

func TestSelectedHistoryForbiddenClearsLastGoodData(t *testing.T) {
	var state selectedHistory
	state.selectPod("default", "api")
	request, _ := state.start()
	state.complete(historyMsg{
		namespace: request.namespace, podName: request.podName, generation: request.generation,
		series: []api.PodHistory{{Namespace: "default", PodName: "api"}},
	}, time.Now())
	retry, _ := state.start()
	state.complete(historyMsg{
		namespace: retry.namespace, podName: retry.podName, generation: retry.generation,
		err: &client.ReadError{Kind: client.ReadErrorForbidden},
	}, time.Now())
	if len(state.series) != 0 || !state.updatedAt.IsZero() {
		t.Fatalf("forbidden history retained last-good data: %#v", state)
	}
}

func TestSelectedHistoryBoundsConcurrentRequestsAndRetainsLastGoodData(t *testing.T) {
	var state selectedHistory
	state.selectPod("default", "api")
	request, ok := state.start()
	if !ok {
		t.Fatal("expected first request")
	}
	if _, duplicate := state.start(); duplicate {
		t.Fatal("started overlapping history request")
	}
	good := []api.PodHistory{{PodName: "api"}}
	state.complete(historyMsg{namespace: request.namespace, podName: request.podName, generation: request.generation, series: good}, time.Now())

	retry, ok := state.start()
	if !ok {
		t.Fatal("expected retry")
	}
	state.complete(historyMsg{namespace: retry.namespace, podName: retry.podName, generation: retry.generation, err: errors.New("temporary")}, time.Now())
	if len(state.series) != 1 || state.err == nil || state.loading {
		t.Fatalf("failed refresh state = %#v", state)
	}
	if state.updatedAt.IsZero() {
		t.Fatal("failed refresh discarded last-good age")
	}
}

func TestChangingSelectedPodCancelsSupersededHistoryRequest(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reader := &cancellableHistoryReader{
		fakeSnapshotReader: &fakeSnapshotReader{},
		started:            make(chan string, 2),
		cancelled:          make(chan string, 2),
	}
	m := newModel(ctx, Options{AllNamespaces: true}, reader, "test")
	m.width, m.height = 180, 50
	m.loading = false
	m.data.Namespaces = []api.NamespaceSnapshot{{Namespace: "default"}}
	m.data.Pods = []api.PodSnapshot{
		{Namespace: "default", PodName: "pod-a", Memory: model.MemoryBreakdown{TotalBytes: 2}},
		{Namespace: "default", PodName: "pod-b", Memory: model.MemoryBreakdown{TotalBytes: 1}},
	}
	m.resizeViewports()
	m.reconcileCurrentViewport("")

	first := m.ensureHistoryTarget()
	if first == nil {
		t.Fatal("first history request was not started")
	}
	go first()
	if started := <-reader.started; started != "pod-a" {
		t.Fatalf("started history for %q, want pod-a", started)
	}

	m.move(1)
	second := m.ensureHistoryTarget()
	if second == nil {
		t.Fatal("second history request was not started")
	}
	select {
	case cancelled := <-reader.cancelled:
		if cancelled != "pod-a" {
			t.Fatalf("cancelled history for %q, want pod-a", cancelled)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("superseded Pod history request was not cancelled")
	}
}

func TestReplacementPodDoesNotReuseDifferentInstanceHistory(t *testing.T) {
	pod := api.PodSnapshot{PodUID: "replacement", NodeName: "node-a"}
	histories := []api.PodHistory{{PodUID: "previous", NodeName: "node-a", Points: []api.MemoryHistoryPoint{{TotalBytes: 1}}}}
	if got := selectHistorySeries(pod, histories); len(got.Points) != 0 {
		t.Fatalf("replacement Pod reused previous history: %#v", got)
	}
}

func TestPausedTickDoesNotStartHistoryRequest(t *testing.T) {
	m := newModel(context.Background(), Options{}, nil, "test")
	m.paused = true
	m.view = viewDetail
	m.detail = entityRef{kind: entityPod, namespace: "default", podName: "api"}
	m.selectedHistory.selectPod("default", "api")

	updated, _ := m.Update(tickMsg(time.Now()))
	m = updated.(appModel)
	if m.selectedHistory.inFlight {
		t.Fatal("paused tick started history request")
	}
}

func TestLeavingDetailClearsSelectedHistoryTarget(t *testing.T) {
	m := newModel(context.Background(), Options{}, nil, "test")
	m.view = viewDetail
	m.detailParent = viewPods
	m.detail = entityRef{kind: entityPod, namespace: "default", podName: "api"}
	m.selectedHistory.selectPod("default", "api")
	m.back()
	m.ensureHistoryTarget()
	if m.selectedHistory.namespace != "" || m.selectedHistory.podName != "" {
		t.Fatalf("history target remains: %#v", m.selectedHistory)
	}
}
