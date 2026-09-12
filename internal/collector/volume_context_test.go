package collector

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/capability"
	"github.com/danushkastanley/kube-memlens/internal/nodecontext"
	"github.com/danushkastanley/kube-memlens/internal/volumecontext"
	"github.com/danushkastanley/kube-memlens/internal/volumehealth"
)

func volumeScope(now time.Time) volumecontext.PodScope {
	return volumecontext.PodScope{Namespace: "tenant-a", PodName: "app", PodUID: "pod-a", NodeName: "node-a", NodeUID: "uid-a", CreatedAt: now.Add(-time.Hour)}
}

func volumeBody(t *testing.T, now time.Time, nodeUID string, used uint64) []byte {
	t.Helper()
	batch, err := volumecontext.NewBatch("node-a", nodeUID, now, volumecontext.SourceState(volumehealth.Reported, ""), []volumecontext.RawUsage{{Namespace: "tenant-a", PodUID: "pod-a", NodeUID: nodeUID, VolumeName: "data", PVCNamespace: "tenant-a", PVCName: "claim", Filesystem: volumecontext.Filesystem{CapturedAt: now, UsedBytes: &used}}}, now)
	if err != nil {
		t.Fatal(err)
	}
	body, err := batch.EncodePrivate()
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestVolumeStorageIsIsolatedFromNodeHistoryAndScope(t *testing.T) {
	now := time.Now().UTC()
	store := NewStore()
	store.now = func() time.Time { return now }
	if err := store.ReconcileNodeIdentities(map[string]string{"node-a": "uid-a"}, now); err != nil {
		t.Fatal(err)
	}
	body := volumeBody(t, now, "uid-a", 123)
	if err := store.ReplaceNodeContextWithVolumes(contextSample(now), body); err != nil {
		t.Fatal(err)
	}
	body[0] = 'x'
	scope := volumeScope(now)
	samples, err := store.VolumeSamples(scope, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(samples.Current) != 1 || *samples.Current[0].Filesystem.UsedBytes != 123 {
		t.Fatal("private storage not isolated from input")
	}
	*samples.Current[0].Filesystem.UsedBytes = 999
	copy, err := store.VolumeSamples(scope, now)
	if err != nil || *copy.Current[0].Filesystem.UsedBytes != 123 {
		t.Fatal("caller mutated cache", err)
	}
	scope.Namespace = "tenant-b"
	hidden, err := store.VolumeSamples(scope, now)
	if err != nil || len(hidden.Current) != 0 || len(hidden.LastGood) != 0 {
		t.Fatal("scope crossed namespace", err)
	}
	history, err := store.PageNodeContextHistory("node-a", now, nil, 8<<20)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(history)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"tenant-a", "claim", "pod-a", `"volumeBatch"`} {
		if strings.Contains(string(encoded), secret) {
			t.Fatal("volume identity entered Node history")
		}
	}
}

func TestVolumeFailureRetentionExpiryAndDisable(t *testing.T) {
	now := time.Now().UTC()
	start := now
	store := NewStore()
	store.now = func() time.Time { return now }
	if err := store.ReconcileNodeIdentities(map[string]string{"node-a": "uid-a"}, now); err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceNodeContextWithVolumes(contextSample(now), volumeBody(t, now, "uid-a", 123)); err != nil {
		t.Fatal(err)
	}
	now = now.Add(15 * time.Second)
	failure := contextSample(now)
	failure.Stats = nil
	failure.Availability = capability.Unavailable
	failure.Reason = nodecontext.TimedOut
	failure.Evidence.CapturedAt = time.Time{}
	failure.Evidence.Freshness = capability.UnknownFreshness
	if err := store.ReplaceNodeContext(failure); err != nil {
		t.Fatal(err)
	}
	samples, err := store.VolumeSamples(volumeScope(start), now)
	if err != nil {
		t.Fatal(err)
	}
	if len(samples.Current) != 0 || len(samples.LastGood) != 1 || !samples.LastGood[0].Filesystem.CapturedAt.Equal(start) || samples.State.Availability != volumehealth.Unavailable {
		t.Fatal("failure refreshed or erased previous evidence")
	}
	now = now.Add(15 * time.Second)
	if err := store.ReplaceNodeContext(contextSample(now)); err != nil {
		t.Fatal(err)
	}
	if store.volumes.bytes != 0 || len(store.volumes.entries) != 0 {
		t.Fatal("disabled producer retained volume data")
	}
	if err := store.ReplaceNodeContextWithVolumes(contextSample(now.Add(time.Second)), volumeBody(t, now.Add(time.Second), "uid-a", 456)); err != nil {
		t.Fatal(err)
	}
	now = now.Add(volumecontext.ExpireAfter + 2*time.Second)
	if err := store.ReconcileNodeIdentities(map[string]string{"node-a": "uid-a"}, now); err != nil {
		t.Fatal(err)
	}
	if store.volumes.bytes != 0 || len(store.volumes.entries) != 0 {
		t.Fatal("expired private data retained")
	}
}

func TestInvalidVolumeDoesNotAdvanceNodeOrHistory(t *testing.T) {
	now := time.Now().UTC()
	store := NewStore()
	store.now = func() time.Time { return now }
	if err := store.ReconcileNodeIdentities(map[string]string{"node-a": "uid-a"}, now); err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceNodeContextWithVolumes(contextSample(now), volumeBody(t, now, "uid-a", 123)); err != nil {
		t.Fatal(err)
	}
	for _, body := range [][]byte{[]byte(`{"records":[{}]}`), volumeBody(t, now.Add(time.Second), "wrong-node", 55)} {
		if err := store.ReplaceNodeContextWithVolumes(contextSample(now.Add(time.Second)), body); err == nil {
			t.Fatal("invalid volume accepted")
		}
		node, _ := store.GetNodeContext("node-a", now)
		if !node.Report.ReportedAt.Equal(now) || store.NodeContextDebug(now).HistoryPoints != 1 {
			t.Fatal("volume rejection partially advanced Node state")
		}
	}
	if err := store.ReconcileNodeIdentities(map[string]string{"node-a": "new-uid"}, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if store.volumes.bytes != 0 {
		t.Fatal("Node replacement retained tenant data")
	}
	if _, err := store.VolumeSamples(volumeScope(now), now.Add(time.Second)); !errors.Is(err, ErrNodeIdentityUnavailable) {
		t.Fatal("old Node scope remained usable", err)
	}
}
