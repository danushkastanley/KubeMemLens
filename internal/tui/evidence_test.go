package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/danushkastanley/kube-memlens/internal/capability"
	"github.com/danushkastanley/kube-memlens/internal/client"
)

func TestFirstFrameDiscoversBeforeRenderingDeepData(t *testing.T) {
	m := newModel(context.Background(), Options{}, nil, "")
	m.width, m.height = 80, 24
	frame := m.viewString()
	if !strings.Contains(frame, "Discovering evidence sources") || strings.Contains(frame, "Loading collector snapshots") {
		t.Fatalf("frame=%s", frame)
	}
	if cmd := m.fetchCmd(); cmd == nil {
		t.Fatal("discovery was not scheduled")
	}
}

func TestDeepFirstFrameUsesSharedEvidenceLabelAtAllSizes(t *testing.T) {
	plan, err := capability.Plan(capability.Deep, []capability.SourceState{{Source: capability.Cgroup, Availability: capability.Available}})
	if err != nil {
		t.Fatal(err)
	}
	for _, width := range []int{40, 80, 120, 180} {
		m := newModel(context.Background(), Options{EvidencePlan: &plan}, &fakeSnapshotReader{}, "collector")
		m.width, m.height = width, 24
		if !strings.Contains(m.viewString(), plan.Label()) {
			t.Fatalf("width=%d frame=%s", width, m.viewString())
		}
	}
}

func TestDiscoveryFailureRemainsInteractiveAndRetryable(t *testing.T) {
	m := newModel(context.Background(), Options{}, nil, "")
	m.width, m.height = 80, 24
	err := &capability.SelectionError{Mode: capability.Restricted, Reason: capability.QueryNotImplemented}
	session := client.EvidenceSession{Plan: capability.Selection{Mode: capability.Restricted}, Description: "Kubernetes APIs"}
	updated, cmd := m.Update(discoveryMsg{generation: m.fetchGeneration, session: session, err: err})
	m = updated.(appModel)
	if cmd != nil || m.loading || m.client != nil || !errors.Is(m.statusErr, err) {
		t.Fatalf("model=%+v", m)
	}
	if !strings.Contains(m.viewString(), "query-not-implemented") {
		t.Fatalf("frame=%s", m.viewString())
	}
	updated, cmd = m.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	m = updated.(appModel)
	if cmd == nil || !m.loading || m.fetchGeneration != 2 {
		t.Fatal("retry did not restart discovery")
	}
}

func TestDiscoveryPinsSuccessfulReaderAndIgnoresSupersededResults(t *testing.T) {
	m := newModel(context.Background(), Options{}, nil, "")
	reader := &fakeSnapshotReader{}
	session := client.EvidenceSession{Reader: reader, Plan: capability.Selection{Mode: capability.Deep}}
	updated, cmd := m.Update(discoveryMsg{generation: 0, session: session})
	if updated.(appModel).client != nil || cmd != nil {
		t.Fatal("accepted obsolete discovery")
	}
	updated, cmd = m.Update(discoveryMsg{generation: 1, session: session})
	m = updated.(appModel)
	if m.client != reader || cmd == nil {
		t.Fatal("reader not installed")
	}
	if _, ok := cmd().(fetchMsg); !ok {
		t.Fatal("successful session rediscovered instead of fetching")
	}
}
