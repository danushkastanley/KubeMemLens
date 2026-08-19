package tui

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
)

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
