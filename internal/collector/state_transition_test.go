package collector

import (
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
)

const transitionTTL = 30 * time.Second

func transitionStore(t *testing.T, startedAt time.Time) *Store {
	t.Helper()
	store := NewStore()
	store.startedAt, store.transitionedAt = startedAt, startedAt
	return store
}

func ingestHealthyNode(t *testing.T, store *Store, node string, capturedAt time.Time) {
	t.Helper()
	_, err := store.ReplaceNodeSnapshot(api.AgentSnapshot{
		NodeName: node, CapturedAt: capturedAt,
		Environment: api.NodeEnvironment{NodeContextAvailable: true, WorkloadContextAvailable: true},
		Containers:  []api.ContainerSnapshot{container("default", node, "app", "id-"+node, capturedAt, 100)},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func assertTransition(t *testing.T, got api.CollectorReliability, state api.CollectorState, at time.Time) {
	t.Helper()
	if got.State != state || !got.TransitionedAt.Equal(at) {
		t.Fatalf("state=%s transitionedAt=%s, want %s at %s", got.State, got.TransitionedAt, state, at)
	}
}

func TestStaleTransitionIsDatedByTheExpiryNotTheFirstRead(t *testing.T) {
	captured := time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC)
	store := transitionStore(t, captured.Add(-time.Minute))
	ingestHealthyNode(t, store, "node-a", captured)

	firstRead := captured.Add(10 * time.Minute)
	assertTransition(t, store.Reliability(firstRead, transitionTTL), api.CollectorStale, captured.Add(transitionTTL))
	assertTransition(t, store.Reliability(firstRead.Add(time.Hour), transitionTTL), api.CollectorStale, captured.Add(transitionTTL))
}

func TestEachExpiryDatesTheStateItCaused(t *testing.T) {
	captured := time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC)
	store := transitionStore(t, captured.Add(-time.Minute))
	ingestHealthyNode(t, store, "node-a", captured)
	ingestHealthyNode(t, store, "node-b", captured.Add(20*time.Second))

	ready := captured.Add(25 * time.Second)
	assertTransition(t, store.Reliability(ready, transitionTTL), api.CollectorReady, ready)

	// node-a expired at +30s and node-b at +50s; neither read lands on an expiry.
	assertTransition(t, store.Reliability(captured.Add(45*time.Second), transitionTTL), api.CollectorDegraded, captured.Add(30*time.Second))
	assertTransition(t, store.Reliability(captured.Add(10*time.Minute), transitionTTL), api.CollectorStale, captured.Add(50*time.Second))
}

func TestReadyToStaleInOneReadIsDatedByTheLastExpiry(t *testing.T) {
	captured := time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC)
	store := transitionStore(t, captured.Add(-time.Minute))
	ingestHealthyNode(t, store, "node-a", captured)
	ingestHealthyNode(t, store, "node-b", captured.Add(20*time.Second))
	store.Reliability(captured.Add(25*time.Second), transitionTTL)

	assertTransition(t, store.Reliability(captured.Add(10*time.Minute), transitionTTL), api.CollectorStale, captured.Add(50*time.Second))
}

func TestRecoveryIsDatedByTheReadThatSawIt(t *testing.T) {
	captured := time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC)
	store := transitionStore(t, captured.Add(-time.Minute))
	ingestHealthyNode(t, store, "node-a", captured)
	ingestHealthyNode(t, store, "node-b", captured)
	store.Reliability(captured.Add(time.Minute), transitionTTL)

	// node-b reports again; node-a stays stale. The expiry is older than the
	// recovery, so it must not date the return to Degraded.
	recovered := captured.Add(5 * time.Minute)
	ingestHealthyNode(t, store, "node-b", recovered)
	assertTransition(t, store.Reliability(recovered, transitionTTL), api.CollectorDegraded, recovered)
}

func TestTransitionNeverPrecedesThePreviousOne(t *testing.T) {
	started := time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC)
	store := transitionStore(t, started)

	// The evidence was already past its TTL before the collector started.
	ingestHealthyNode(t, store, "node-a", started.Add(-time.Hour))
	assertTransition(t, store.Reliability(started.Add(time.Minute), transitionTTL), api.CollectorStale, started)
}
