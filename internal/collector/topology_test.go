package collector

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/capability"
	"github.com/danushkastanley/kube-memlens/internal/memorytopology"
	"github.com/danushkastanley/kube-memlens/internal/nodecontext"
)

func topologySample(at time.Time) memorytopology.Observation {
	o := memorytopology.Failure("node-a", "uid-a", at)
	total, free, surplus, reserved := uint64(8), uint64(6), uint64(2), uint64(1)
	o.Pools = memorytopology.Section[memorytopology.HugePool]{Source: memorytopology.HugeTLBSysfs, Availability: capability.Available, Completeness: capability.Complete, CapturedAt: at, Items: []memorytopology.HugePool{{PageSizeBytes: 2 << 20, TotalPages: &total, FreePages: &free, SurplusPages: &surplus, ReservedPages: &reserved}}}
	return o
}
func topologyBytes(t *testing.T, o memorytopology.Observation) []byte {
	t.Helper()
	raw, err := json.Marshal(o)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
func TestTopologyAtomicAdmissionAndIsolation(t *testing.T) {
	now := time.Now().UTC()
	store := NewStore()
	store.now = func() time.Time { return now }
	if err := store.ReconcileNodeIdentities(map[string]string{"node-a": "uid-a"}, now); err != nil {
		t.Fatal(err)
	}
	node := contextSample(now)
	o := topologySample(now)
	raw := topologyBytes(t, o)
	if err := store.ReplaceNodeEvidence(node, nil, raw); err != nil {
		t.Fatal(err)
	}
	original, _ := store.GetNodeContext("node-a", now)
	history, _ := store.PageNodeContextHistory("node-a", now, nil, 8<<20)
	record, found, err := store.NodeTopology("node-a", now)
	if err != nil || !found || record.Current.Pools[0].InUseBytes == nil || *record.Current.Pools[0].InUseBytes != 4<<20 {
		t.Fatalf("missing topology: %+v %v", record, err)
	}
	*record.Current.Pools[0].InUseBytes = 0
	raw[0] = 'x'
	*o.Pools.Items[0].TotalPages = 1234
	again, _, _ := store.NodeTopology("node-a", now)
	if *again.Current.Pools[0].InUseBytes != 4<<20 {
		t.Fatal("caller changed stored source")
	}
	for _, mutate := range []func(*memorytopology.Observation){
		func(o *memorytopology.Observation) { o.NodeUID = "another" },
		func(o *memorytopology.Observation) { o.NodeName = "another" },
		func(o *memorytopology.Observation) { o.ReportedAt = o.ReportedAt.Add(-time.Second) },
		func(o *memorytopology.Observation) { o.Pools.CapturedAt = now.Add(-time.Minute) },
	} {
		bad := topologySample(now.Add(time.Second))
		mutate(&bad)
		if err := store.ReplaceNodeEvidence(contextSample(now.Add(time.Second)), nil, topologyBytes(t, bad)); err == nil {
			t.Fatal("invalid topology admitted")
		}
		after, _ := store.GetNodeContext("node-a", now)
		afterHistory, _ := store.PageNodeContextHistory("node-a", now, nil, 8<<20)
		if !reflect.DeepEqual(original, after) || !reflect.DeepEqual(history, afterHistory) {
			t.Fatal("rejection changed ordinary evidence")
		}
	}
	if _, err := store.ReplaceNodeSnapshot(api.AgentSnapshot{NodeName: "node-a", Topology: topologyBytes(t, topologySample(now))}); err == nil {
		t.Fatal("standard producer admitted topology")
	}
}
func TestTopologyFailureAgeRevocationAndReplacement(t *testing.T) {
	now := time.Now().UTC()
	start := now
	store := NewStore()
	store.now = func() time.Time { return now }
	reconcile := func(uid string) {
		t.Helper()
		if err := store.ReconcileNodeIdentities(map[string]string{"node-a": uid}, now); err != nil {
			t.Fatal(err)
		}
	}
	reconcile("uid-a")
	if err := store.ReplaceNodeEvidence(contextSample(now), nil, topologyBytes(t, topologySample(now))); err != nil {
		t.Fatal(err)
	}
	now = now.Add(15 * time.Second)
	failure := contextSample(now)
	failure.Stats = nil
	failure.Availability = capability.Unavailable
	failure.Reason = nodecontext.TimedOut
	failure.Evidence.CapturedAt = time.Time{}
	failure.Evidence.Freshness = capability.UnknownFreshness
	if err := store.ReplaceNodeEvidence(failure, nil, nil); err != nil {
		t.Fatal(err)
	}
	now = now.Add(35 * time.Second)
	reconcile("uid-a")
	record, _, err := store.NodeTopology("node-a", now)
	if err != nil || record.Current.PoolState != "unavailable" || record.LastGood.PoolState != "stale" || !record.LastGood.Observation.Pools.CapturedAt.Equal(start) {
		t.Fatalf("failed source refreshed good clock: %+v %v", record, err)
	}
	if store.NodeContextDebug(now).HistoryPoints != 1 {
		t.Fatal("topology created ordinary history")
	}
	now = now.Add(time.Second)
	reconcile("new-uid")
	if _, found, err := store.NodeTopology("node-a", now); err != nil || found {
		t.Fatal("replacement exposed previous UID")
	}
	if _, _, err := store.NodeTopology("node-a", now.Add(time.Minute)); !errors.Is(err, ErrNodeIdentityUnavailable) {
		t.Fatal("expired inventory allowed read")
	}
}
func TestTopologySourceReplayAndDisable(t *testing.T) {
	now := time.Now().UTC()
	store := NewStore()
	store.now = func() time.Time { return now }
	if err := store.ReconcileNodeIdentities(map[string]string{"node-a": "uid-a"}, now); err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceNodeEvidence(contextSample(now), nil, topologyBytes(t, topologySample(now))); err != nil {
		t.Fatal(err)
	}
	next := topologySample(now.Add(time.Second))
	next.Pools.CapturedAt = now.Add(-time.Second)
	if err := store.ReplaceNodeEvidence(contextSample(next.ReportedAt), nil, topologyBytes(t, next)); !errors.Is(err, ErrSnapshotOutOfOrder) {
		t.Fatalf("source replay accepted: %v", err)
	}
	if err := store.ReplaceNodeEvidence(contextSample(now.Add(time.Second)), nil, nil); err != nil {
		t.Fatal(err)
	}
	result, _, err := store.NodeTopology("node-a", now)
	if err != nil || result.Current != nil || result.LastGood != nil {
		t.Fatal("disabled profile retained data")
	}
}

func TestTopologyFailureBeforeFirstSampleHasNoLastGood(t *testing.T) {
	now := time.Now().UTC()
	store := NewStore()
	store.now = func() time.Time { return now }
	if err := store.ReconcileNodeIdentities(map[string]string{"node-a": "uid-a"}, now); err != nil {
		t.Fatal(err)
	}
	failure := memorytopology.Failure("node-a", "uid-a", now)
	if err := store.ReplaceNodeEvidence(contextSample(now), nil, topologyBytes(t, failure)); err != nil {
		t.Fatal(err)
	}
	result, found, err := store.NodeTopology("node-a", now)
	if err != nil || !found || result.Current == nil || result.LastGood != nil {
		t.Fatal("failed first read invented last-good evidence")
	}
}
