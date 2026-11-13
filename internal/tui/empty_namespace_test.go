package tui

import (
	"context"
	"strings"
	"testing"
)

func emptyFetchFrame(t *testing.T, opts Options) string {
	t.Helper()
	m := newModel(context.Background(), opts, &fakeSnapshotReader{}, "test")
	m.width, m.height = 100, 24
	message := m.fetchCmd()().(fetchMsg)
	if message.err != nil {
		t.Fatalf("fetch: %v", message.err)
	}
	updated, _ := m.Update(message)
	return updated.(appModel).viewString()
}

func TestEmptyNamespaceIsNotReportedAsCollectorRebuilding(t *testing.T) {
	frame := emptyFetchFrame(t, Options{Namespace: "default"})
	if !strings.Contains(frame, "No Pods in namespace default are reporting memory.") {
		t.Fatalf("empty namespace is not named:\n%s", frame)
	}
	for _, misleading := range []string{"rebuilding", "No collector snapshots", "go run"} {
		if strings.Contains(frame, misleading) {
			t.Fatalf("empty namespace frame contains %q:\n%s", misleading, frame)
		}
	}
	for _, hint := range []string{"-A", "kubectl memlens status"} {
		if !strings.Contains(frame, hint) {
			t.Fatalf("empty namespace frame lacks the %q hint:\n%s", hint, frame)
		}
	}
}

func TestEmptyClusterKeepsCollectorStateAndPluginHint(t *testing.T) {
	m := newModel(context.Background(), Options{AllNamespaces: true}, &fakeSnapshotReader{}, "test")
	m.width, m.height, m.loading = 100, 24, false
	frame := m.viewString()
	if !strings.Contains(frame, "No collector snapshots are available yet. Collector state: rebuilding.") {
		t.Fatalf("empty cluster frame:\n%s", frame)
	}
	if !strings.Contains(frame, "kubectl memlens status") || strings.Contains(frame, "go run") {
		t.Fatalf("empty cluster frame has the wrong status hint:\n%s", frame)
	}
}
