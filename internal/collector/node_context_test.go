package collector

import (
	"errors"
	"net/url"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/capability"
	"github.com/danushkastanley/kube-memlens/internal/nodecontext"
)

func contextSample(at time.Time) nodecontext.Observation {
	usage := uint64(123)
	return nodecontext.Observation{NodeName: "node-a", NodeUID: "uid-a", ReportedAt: at, Availability: capability.Available,
		Evidence: capability.Envelope{Source: nodecontext.Source, APIVersion: "v1alpha1", CapturedAt: at, ReceivedAt: at,
			Scope: capability.NodeScope, Freshness: capability.Fresh, Completeness: capability.Partial, Stability: capability.ImplementationSpecific},
		Stats: &nodecontext.Stats{StartedAt: at.Add(-time.Hour), Provenance: nodecontext.Unknown, Memory: &nodecontext.Memory{CapturedAt: at, UsageBytes: &usage}}}
}

func TestNodeContextFailureRetainsSampleWithoutRefreshingHistory(t *testing.T) {
	now := time.Now().UTC()
	store := NewStore()
	store.now = func() time.Time { return now }
	if err := store.ReconcileNodeIdentities(map[string]string{"node-a": "uid-a"}, now); err != nil {
		t.Fatal(err)
	}
	sample := contextSample(now)
	if err := store.ReplaceNodeContext(sample); err != nil {
		t.Fatal(err)
	}
	*sample.Stats.Memory.UsageBytes = 999 // Caller cannot mutate retained evidence.
	now = now.Add(15 * time.Second)
	failure := contextSample(now)
	failure.Stats = nil
	failure.Availability, failure.Reason = capability.Unavailable, nodecontext.TimedOut
	failure.Evidence.CapturedAt, failure.Evidence.Freshness = time.Time{}, capability.UnknownFreshness
	if err := store.ReplaceNodeContext(failure); err != nil {
		t.Fatal(err)
	}
	record, ok := store.GetNodeContext("node-a", now.Add(time.Minute))
	if !ok || record.Report.Reason != nodecontext.TimedOut || record.LastGood == nil ||
		*record.LastGood.Stats.Memory.UsageBytes != 123 || record.Freshness != capability.Stale || !record.LastGood.ReportedAt.Equal(sample.ReportedAt) {
		t.Fatalf("failure changed evidence: %#v", record)
	}
	history, err := store.PageNodeContextHistory("node-a", now, nil, 8<<20)
	if err != nil || len(history.Series) != 1 || len(history.Series[0].Points) != 1 {
		t.Fatalf("history=%#v err=%v", history, err)
	}
}

func TestNodeContextIdentityAndRestart(t *testing.T) {
	now := time.Now().UTC()
	store := NewStore()
	store.now = func() time.Time { return now }
	sample := contextSample(now)
	if err := store.ReplaceNodeContext(sample); !errors.Is(err, ErrNodeIdentityUnavailable) {
		t.Fatalf("unknown inventory accepted: %v", err)
	}
	if err := store.ReconcileNodeIdentities(map[string]string{"node-a": "uid-a"}, now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReplaceAuthenticatedNodeSnapshot(api.AgentSnapshot{NodeName: "node-a", CapturedAt: now, Containers: []api.ContainerSnapshot{{ContainerID: "cgroup-a"}}}, "uid-a"); err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceNodeContext(sample); err != nil {
		t.Fatal(err)
	}
	if len(store.ListContainers(now, time.Minute)) != 1 {
		t.Fatal("Node post cleared cgroup data")
	}
	if err := store.ReconcileNodeIdentities(map[string]string{"node-a": "uid-b"}, now); err != nil {
		t.Fatal(err)
	}
	if len(store.ListContainers(now, time.Minute)) != 0 {
		t.Fatal("name reuse retained cgroup identity")
	}
	record, _ := store.GetNodeContext("node-a", now)
	if record.Report != nil || record.Freshness != capability.Rebuilding || record.NodeUID != "uid-b" {
		t.Fatal("name reuse retained current Node observation")
	}
	if err := store.ReplaceNodeContext(sample); !errors.Is(err, ErrNodeIdentityUnavailable) {
		t.Fatalf("retired UID accepted: %v", err)
	}
	fresh := NewStore()
	if err := fresh.ReconcileNodeIdentities(map[string]string{"node-a": "uid-b"}, now); err != nil {
		t.Fatal(err)
	}
	history, err := fresh.PageNodeContextHistory("node-a", now, nil, 8<<20)
	if err != nil || len(history.Series) != 0 || history.Completeness != capability.Partial || history.ResetAt.IsZero() {
		t.Fatal("restart claimed complete history")
	}
}

func TestNodeContextPaginationAndStaleAdmission(t *testing.T) {
	now := time.Now().UTC()
	store := NewStore()
	store.now = func() time.Time { return now }
	if err := store.ReconcileNodeIdentities(map[string]string{"node-a": "uid-a", "node-b": "uid-b"}, now); err != nil {
		t.Fatal(err)
	}
	first, continuation, err := store.PageNodeContexts(now, url.Values{"limit": {"1"}})
	if err != nil || len(first) != 1 || continuation == "" {
		t.Fatal("first page failed", err)
	}
	second, continuation, err := store.PageNodeContexts(now, url.Values{"limit": {"1"}, "continue": {continuation}})
	if err != nil || len(second) != 1 || continuation != "" || first[0].NodeName == second[0].NodeName {
		t.Fatal("continuation failed", err)
	}
	if _, _, err := store.PageNodeContexts(now, url.Values{"limit": {"101"}}); err == nil {
		t.Fatal("unbounded page accepted")
	}
	now = now.Add(time.Minute)
	if err := store.ReplaceNodeContext(contextSample(now)); !errors.Is(err, ErrNodeIdentityUnavailable) {
		t.Fatalf("stale inventory accepted: %v", err)
	}
}

func TestNodeContextHistoryPointLimitAndDuplicateSample(t *testing.T) {
	now := time.Now().UTC()
	store := NewStore()
	store.now = func() time.Time { return now }
	for i := 0; i < 70; i++ {
		now = now.Add(time.Second)
		if err := store.ReconcileNodeIdentities(map[string]string{"node-a": "uid-a"}, now); err != nil {
			t.Fatal(err)
		}
		if err := store.ReplaceNodeContext(contextSample(now)); err != nil {
			t.Fatal(err)
		}
	}
	debug := store.NodeContextDebug(now)
	if debug.HistoryPoints != nodecontext.MaxHistoryPoints || !debug.CoverageLost || debug.HistoryBytes > nodecontext.MaxHistoryBytes {
		t.Fatalf("limits=%#v", debug)
	}
	value := contextSample(now)
	value.ReportedAt = now.Add(time.Second)
	if err := store.ReplaceNodeContext(value); err != nil {
		t.Fatal(err)
	}
	if got := store.NodeContextDebug(now); got.HistoryPoints != debug.HistoryPoints || got.HistoryBytes != debug.HistoryBytes {
		t.Fatal("duplicate source sample entered history")
	}
}

func TestNodeContextSharesOverallNodeCapacityWithCgroups(t *testing.T) {
	now := time.Now().UTC()
	store := NewStoreWithHistoryAndLimits(DefaultHistoryOptions(), StoreLimits{MaxNodes: 1, MaxContainers: 10})
	store.now = func() time.Time { return now }
	if err := store.ReconcileNodeIdentities(map[string]string{"node-a": "uid-a"}, now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReplaceNodeSnapshot(api.AgentSnapshot{NodeName: "node-b", CapturedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceNodeContext(contextSample(now)); !errors.Is(err, ErrStoreCapacity) {
		t.Fatalf("second source bypassed Node ceiling: %v", err)
	}
	if err := store.ReconcileNodeIdentities(map[string]string{"node-a": "uid-a"}, now); err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceNodeContext(contextSample(now)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReplaceNodeSnapshot(api.AgentSnapshot{NodeName: "node-b", CapturedAt: now}); !errors.Is(err, ErrStoreCapacity) {
		t.Fatalf("cgroup source bypassed Node ceiling: %v", err)
	}
}

func TestNodeContextHistoryCompletenessRequiresContinuousCurrentInstance(t *testing.T) {
	now := time.Now().UTC()
	store := NewStore()
	store.now = func() time.Time { return now }
	store.startedAt = now.Add(-2 * nodecontext.HistoryDuration)
	start := now.Add(-nodecontext.HistoryDuration)
	for i := 0; i <= 60; i++ {
		now = start.Add(time.Duration(i) * 15 * time.Second)
		if err := store.ReconcileNodeIdentities(map[string]string{"node-a": "uid-a"}, now); err != nil {
			t.Fatal(err)
		}
		if err := store.ReplaceNodeContext(contextSample(now)); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.ReconcileNodeIdentities(map[string]string{"node-a": "uid-a"}, now); err != nil {
		t.Fatal(err)
	}
	history, err := store.PageNodeContextHistory("node-a", now, nil, 8<<20)
	if err != nil || history.Completeness != capability.Complete {
		t.Fatalf("continuous history=%s err=%v", history.Completeness, err)
	}
	history, err = store.PageNodeContextHistory("node-a", now.Add(time.Minute), nil, 8<<20)
	if err != nil || history.Completeness != capability.Partial {
		t.Fatal("missing recent history claimed complete")
	}
}

func TestNodeContextOrdersEveryClockAndRecordsAdvancingFields(t *testing.T) {
	now := time.Now().UTC()
	store := NewStore()
	store.now = func() time.Time { return now }
	if err := store.ReconcileNodeIdentities(map[string]string{"node-a": "uid-a"}, now); err != nil {
		t.Fatal(err)
	}
	value := contextSample(now)
	value.Evidence.CapturedAt = now.Add(-30 * time.Second)
	zero := uint64(0)
	value.Stats.SystemContainers = []nodecontext.SystemContainer{{Category: nodecontext.Runtime,
		Memory: &nodecontext.Memory{CapturedAt: value.Evidence.CapturedAt, UsageBytes: &zero}}}
	if err := store.ReplaceNodeContext(value); err != nil {
		t.Fatal(err)
	}
	now = now.Add(5 * time.Second)
	value = contextSample(now)
	value.Evidence.CapturedAt = now.Add(-35 * time.Second)
	value.Stats.SystemContainers = []nodecontext.SystemContainer{{Category: nodecontext.Runtime,
		Memory: &nodecontext.Memory{CapturedAt: value.Evidence.CapturedAt, UsageBytes: &zero}}}
	if err := store.ReplaceNodeContext(value); err != nil {
		t.Fatal(err)
	}
	if got := store.NodeContextDebug(now); got.HistoryPoints != 2 {
		t.Fatal("unchanged oldest system clock hid advancing Node memory")
	}
	now = now.Add(5 * time.Second)
	value.ReportedAt, value.Evidence.ReceivedAt = now, now
	value.Stats.Memory.CapturedAt = now.Add(-6 * time.Second) // Regresses although the oldest system timestamp advances.
	value.Evidence.CapturedAt = now.Add(-39 * time.Second)
	value.Stats.SystemContainers[0].Memory.CapturedAt = value.Evidence.CapturedAt
	if err := store.ReplaceNodeContext(value); !errors.Is(err, ErrSnapshotOutOfOrder) {
		t.Fatalf("regressing Node clock accepted: %v", err)
	}
}

func TestNodeContextNewOptionalClockMayPredateOldestKnownClock(t *testing.T) {
	now := time.Now().UTC()
	store := NewStore()
	store.now = func() time.Time { return now }
	if err := store.ReconcileNodeIdentities(map[string]string{"node-a": "uid-a"}, now); err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceNodeContext(contextSample(now)); err != nil {
		t.Fatal(err)
	}
	now = now.Add(15 * time.Second)
	value := contextSample(now)
	value.Evidence.CapturedAt = now.Add(-20 * time.Second)
	zero := uint64(0)
	value.Stats.Swap = &nodecontext.Swap{CapturedAt: value.Evidence.CapturedAt, UsageBytes: &zero}
	if err := store.ReplaceNodeContext(value); err != nil {
		t.Fatal("new optional field was treated as a replay", err)
	}
	history, err := store.PageNodeContextHistory("node-a", now, nil, 8<<20)
	if err != nil || len(history.Series[0].Points) != 2 {
		t.Fatal("new optional evidence missing from history", err)
	}
	points := history.Series[0].Points
	if !points[1].ReceivedAt.After(points[0].ReceivedAt) || !points[1].Observation.Stats.Swap.CapturedAt.Equal(value.Evidence.CapturedAt) {
		t.Fatal("independent source and receipt times were lost")
	}
}
