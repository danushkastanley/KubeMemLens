package tui

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/danushkastanley/kube-memlens/internal/client"
)

func TestSummaryOnlyDataBlocksContainerDependentActions(t *testing.T) {
	reader := &summarySnapshotReader{inner: tuiFixtureReader()}
	m := newModel(context.Background(), Options{AllNamespaces: true}, reader, "test")
	message := m.fetchCmd()().(fetchMsg)
	updated, _ := m.Update(message)
	m = updated.(appModel)
	if m.data.ContainersLoaded {
		t.Fatal("summary fetch unexpectedly marked containers loaded")
	}

	for name, start := range map[string]func() tea.Cmd{
		"recommendation": func() tea.Cmd { return m.startRecommendation() },
		"comparison":     func() tea.Cmd { return m.startCompare() },
		"capture":        func() tea.Cmd { return m.startCapture(false) },
	} {
		m.action = actionState{}
		m.containerLoading = false
		command := start()
		if command == nil || m.action.err == nil || !strings.Contains(m.action.err.Error(), "complete container evidence") {
			t.Fatalf("%s was not blocked while container evidence loaded: command=%v error=%v", name, command, m.action.err)
		}
	}
}

func TestSameGenerationCompleteFetchCannotRepopulateRevokedEvidence(t *testing.T) {
	reader := tuiFixtureReader()
	m := newModel(context.Background(), Options{AllNamespaces: true}, reader, "test")
	m.fetchGeneration = 2
	m.data = snapshotData{Pods: reader.current.Pods, Containers: reader.current.Containers, ContainersLoaded: true}
	updated, _ := m.Update(fetchMsg{generation: 2, err: &client.ReadError{Kind: client.ReadErrorForbidden}})
	m = updated.(appModel)
	if m.data.ContainersLoaded || len(m.data.Pods) != 0 || len(m.data.Containers) != 0 {
		t.Fatalf("forbidden refresh retained evidence: %#v", m.data)
	}
	updated, _ = m.Update(completeFetchMsg{generation: 2, data: reader.current})
	m = updated.(appModel)
	if m.data.ContainersLoaded || len(m.data.Pods) != 0 || len(m.data.Containers) != 0 {
		t.Fatalf("stale complete fetch repopulated cleared evidence: %#v", m.data)
	}
}

func TestForbiddenCompleteFetchClearsPreviouslyLoadedEvidence(t *testing.T) {
	reader := tuiFixtureReader()
	m := newModel(context.Background(), Options{AllNamespaces: true}, reader, "test")
	m.fetchGeneration = 2
	m.data = snapshotData{Pods: reader.current.Pods, Containers: reader.current.Containers, ContainersLoaded: true}
	updated, _ := m.Update(completeFetchMsg{
		generation: 2,
		err:        &client.ReadError{Kind: client.ReadErrorForbidden},
	})
	m = updated.(appModel)
	if m.data.ContainersLoaded || len(m.data.Pods) != 0 || len(m.data.Containers) != 0 || !client.IsForbidden(m.statusErr) {
		t.Fatalf("forbidden complete fetch retained evidence: data=%#v error=%v", m.data, m.statusErr)
	}
}

func TestContainerDependentViewsRenderLoadingAndFailureStates(t *testing.T) {
	m := loadedFixtureModel(t, 100, 24)
	m.view = viewContainers
	m.data.ContainersLoaded = false
	m.data.Containers = nil
	if frame := m.viewString(); !strings.Contains(frame, "Loading complete container evidence") {
		t.Fatalf("container loading frame:\n%s", frame)
	}
	m.containerErr = &client.ReadError{Kind: client.ReadErrorUnavailable}
	if frame := m.viewString(); !strings.Contains(frame, "Complete container evidence is unavailable") {
		t.Fatalf("container failure frame:\n%s", frame)
	}
}
