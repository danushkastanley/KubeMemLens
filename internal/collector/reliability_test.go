package collector

import (
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
)

func TestReliabilityStateMachine(t *testing.T) {
	now := time.Date(2026, 8, 26, 10, 0, 0, 0, time.UTC)
	store := NewStore()
	store.startedAt = now.Add(-time.Minute)
	store.transitionedAt = store.startedAt

	assertReliability(t, store.Reliability(now, 30*time.Second), api.CollectorRebuilding, api.EvidencePartial, 0, 0)
	healthy := api.NodeEnvironment{NodeContextAvailable: true, WorkloadContextAvailable: true}
	_, err := store.ReplaceNodeSnapshot(api.AgentSnapshot{
		NodeName: "node-a", CapturedAt: now,
		Environment: healthy,
		Containers:  []api.ContainerSnapshot{container("default", "api", "app", "id-a", now, 100)},
	})
	if err != nil {
		t.Fatal(err)
	}
	assertReliability(t, store.Reliability(now, 30*time.Second), api.CollectorReady, api.EvidenceComplete, 1, 0)

	_, err = store.ReplaceNodeSnapshot(api.AgentSnapshot{
		NodeName: "node-b", CapturedAt: now.Add(-31 * time.Second),
		Environment: healthy,
		Containers:  []api.ContainerSnapshot{container("default", "worker", "app", "id-b", now.Add(-31*time.Second), 100)},
	})
	if err != nil {
		t.Fatal(err)
	}
	assertReliability(t, store.Reliability(now, 30*time.Second), api.CollectorDegraded, api.EvidencePartial, 1, 1)
	assertReliability(t, store.Reliability(now.Add(31*time.Second), 30*time.Second), api.CollectorStale, api.EvidencePartial, 0, 2)
}

func TestReliabilityMarksIncompleteNodeEvidenceDegraded(t *testing.T) {
	now := time.Now().UTC()
	store := NewStore()
	_, err := store.ReplaceNodeSnapshot(api.AgentSnapshot{
		NodeName: "node-a", CapturedAt: now,
		Environment: api.NodeEnvironment{NodeContextAvailable: true, WorkloadContextErrors: 1},
		Containers:  []api.ContainerSnapshot{container("default", "api", "app", "id-a", now, 100)},
	})
	if err != nil {
		t.Fatal(err)
	}
	assertReliability(t, store.Reliability(now, time.Minute), api.CollectorDegraded, api.EvidencePartial, 1, 0)
	containers := store.ListContainers(now, time.Minute)
	pods := store.ListPods(now, time.Minute)
	if len(containers) != 1 || containers[0].Completeness != api.EvidencePartial || len(pods) != 1 || pods[0].Completeness != api.EvidencePartial {
		t.Fatalf("partial node evidence did not reach authorised rows: containers=%#v pods=%#v", containers, pods)
	}
}

func TestReliabilityKeepsExistingNodeOutageDegradedUntilInventoryRemovesNode(t *testing.T) {
	now := time.Now().UTC()
	ttl := 30 * time.Second
	healthy := api.NodeEnvironment{NodeContextAvailable: true, WorkloadContextAvailable: true}
	store := NewStore()
	_ = store.ReconcileExpectedNodes([]string{"node-a", "node-b"}, now)
	_, _ = store.ReplaceNodeSnapshot(api.AgentSnapshot{
		NodeName: "node-b", CapturedAt: now, Environment: healthy,
		Containers: []api.ContainerSnapshot{container("default", "old", "app", "old-id", now, 100)},
	})
	_, _ = store.ReplaceNodeSnapshot(api.AgentSnapshot{
		NodeName: "node-a", CapturedAt: now.Add(4 * ttl), Environment: healthy,
		Containers: []api.ContainerSnapshot{container("default", "new", "app", "new-id", now.Add(4*ttl), 100)},
	})

	reliability := store.Reliability(now.Add(4*ttl), ttl)
	assertReliability(t, reliability, api.CollectorDegraded, api.EvidencePartial, 1, 1)
	if reliability.ExpectedNodes != 2 || reliability.MissingNodes != 0 {
		t.Fatalf("expected-node coverage = %#v", reliability)
	}

	_ = store.ReconcileExpectedNodes([]string{"node-a"}, now.Add(4*ttl))
	assertReliability(t, store.Reliability(now.Add(4*ttl), ttl), api.CollectorReady, api.EvidenceComplete, 1, 0)
	containers := store.ListContainers(now.Add(4*ttl), ttl)
	if len(containers) != 1 || containers[0].NodeName != "node-a" {
		t.Fatalf("removed node evidence remains: %#v", containers)
	}
}

func assertReliability(t *testing.T, got api.CollectorReliability, state api.CollectorState, completeness api.EvidenceCompleteness, fresh, stale int) {
	t.Helper()
	if got.State != state || got.Completeness != completeness || got.FreshNodes != fresh || got.StaleNodes != stale {
		t.Fatalf("reliability=%#v", got)
	}
	if got.Generation == "" || got.StartedAt.IsZero() || got.TransitionedAt.IsZero() {
		t.Fatalf("missing restart identity or transition timing: %#v", got)
	}
}
