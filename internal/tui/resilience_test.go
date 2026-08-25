package tui

import (
	"errors"
	"strings"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/client"
)

func TestConnectionFailureRetainsLastGoodFrameAndRecoveryClearsError(t *testing.T) {
	m := loadedFixtureModel(t, 120, 30)
	previousPods := len(m.data.Pods)

	updated, _ := m.Update(fetchMsg{err: errors.New("connection interrupted")})
	m = updated.(appModel)
	frame := m.viewString()
	if len(m.data.Pods) != previousPods || !strings.Contains(frame, "api-0") || !strings.Contains(frame, "connection error") || !strings.Contains(frame, "state: unavailable") {
		t.Fatalf("failure did not retain data and expose error:\n%s", frame)
	}

	recovered := tuiFixtureReader().current
	updated, _ = m.Update(fetchMsg{data: snapshotData{
		Pods: recovered.Pods, Containers: recovered.Containers,
		Namespaces: recovered.Namespaces, Workloads: recovered.Workloads,
	}})
	m = updated.(appModel)
	if m.statusErr != nil || !strings.Contains(m.viewString(), "api-0") {
		t.Fatalf("recovery state err=%v frame:\n%s", m.statusErr, m.viewString())
	}
}

func TestStaleAndPartialCollectorStatesRemainDistinct(t *testing.T) {
	m := loadedFixtureModel(t, 160, 30)
	m.data.Reliability = api.CollectorReliability{State: api.CollectorStale, Completeness: api.EvidencePartial}
	if frame := m.viewString(); !strings.Contains(frame, "state: stale") {
		t.Fatalf("stale frame:\n%s", frame)
	}
	if command := m.startRecommendation(); command != nil || m.action.err == nil || !strings.Contains(m.action.err.Error(), "collector state is stale") {
		t.Fatalf("stale recommendation was not blocked: command=%v error=%v", command, m.action.err)
	}

	m.action = actionState{}
	m.data.Reliability.State = api.CollectorDegraded
	if frame := m.viewString(); !strings.Contains(frame, "state: degraded") {
		t.Fatalf("degraded frame:\n%s", frame)
	}
}

func TestNamespaceInferencePreservesPartialFreshEvidence(t *testing.T) {
	reliability := inferReliability(snapshotData{Pods: []api.PodSnapshot{{
		Freshness: api.EvidenceFreshnessFresh, Completeness: api.EvidencePartial,
	}}})
	if reliability.State != api.CollectorDegraded || reliability.Completeness != api.EvidencePartial {
		t.Fatalf("partial namespace reliability = %#v", reliability)
	}
}

func TestForbiddenRefreshClearsPreviouslyAuthorisedData(t *testing.T) {
	m := loadedFixtureModel(t, 120, 30)
	m.selectedHistory.selectPod("default", "api-0")
	m.selectedHistory.series = []api.PodHistory{{Namespace: "default", PodName: "api-0"}}
	pod := m.data.Pods[0]
	m.action.compareSource = &pod

	updated, _ := m.Update(fetchMsg{err: &client.ReadError{Kind: client.ReadErrorForbidden}})
	m = updated.(appModel)
	if len(m.data.Pods) != 0 || len(m.data.Containers) != 0 || len(m.selectedHistory.series) != 0 || m.action.compareSource != nil {
		t.Fatalf("forbidden refresh retained authorised state: data=%#v history=%#v action=%#v", m.data, m.selectedHistory, m.action)
	}
	frame := m.viewString()
	if !strings.Contains(frame, "state: access revoked") || !strings.Contains(frame, "status: permission denied") || strings.Contains(frame, "connection error") {
		t.Fatalf("forbidden refresh has misleading state:\n%s", frame)
	}
}

func TestFilteredEmptyFrameNamesFilterAndReset(t *testing.T) {
	m := loadedFixtureModel(t, 80, 24)
	m.query = "does-not-exist"
	frame := m.viewString()
	for _, want := range []string{"does-not-exist", "Press Esc to clear it"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("empty filter frame missing %q:\n%s", want, frame)
		}
	}
}

func TestStaleSnapshotResponseCannotReplaceNewerData(t *testing.T) {
	m := loadedFixtureModel(t, 120, 30)
	m.loading = false
	command := m.beginFetch()
	if command == nil {
		t.Fatal("current snapshot refresh did not start")
	}
	currentGeneration := m.fetchGeneration
	updated, _ := m.Update(fetchMsg{
		generation: currentGeneration,
		data:       snapshotData{Pods: m.data.Pods, Namespaces: m.data.Namespaces},
	})
	m = updated.(appModel)
	updated, _ = m.Update(fetchMsg{
		generation: currentGeneration - 1,
		data:       snapshotData{},
	})
	m = updated.(appModel)
	if len(m.data.Pods) == 0 {
		t.Fatal("stale snapshot response replaced current data")
	}
}
