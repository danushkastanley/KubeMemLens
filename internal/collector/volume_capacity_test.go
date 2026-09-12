package collector

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/volumecontext"
	"github.com/danushkastanley/kube-memlens/internal/volumehealth"
)

func TestVolumeRetentionCapacityRejectsWithoutPartialNodeAdvance(t *testing.T) {
	for _, health := range []bool{false, true} {
		t.Run(fmt.Sprintf("health-%t", health), func(t *testing.T) { checkVolumeRetentionCapacity(t, health) })
	}
}

func checkVolumeRetentionCapacity(t *testing.T, health bool) {
	now := time.Now().UTC()
	store := NewStore()
	if health {
		if err := store.ReserveVolumeHealth(); err != nil {
			t.Fatal(err)
		}
	}
	limit := volumecontext.MaxRetainedBytes
	if health {
		limit -= volumecontext.MaxHealthBytes
	}
	store.now = func() time.Time { return now }
	identities := map[string]string{}
	for i := 0; i < 128; i++ {
		identities[fmt.Sprintf("node-%d", i)] = fmt.Sprintf("uid-%d", i)
	}
	if err := store.ReconcileNodeIdentities(identities, now); err != nil {
		t.Fatal(err)
	}
	used := uint64(1)
	rows := make([]volumecontext.RawUsage, volumecontext.MaxBatchRecords)
	for i := range rows {
		rows[i] = volumecontext.RawUsage{Namespace: "tenant-a", PodUID: fmt.Sprintf("pod-%d", i), VolumeName: "data", PVCNamespace: "tenant-a", PVCName: strings.Repeat("a", 253), Filesystem: volumecontext.Filesystem{CapturedAt: now, UsedBytes: &used}}
	}
	accepted := 0
	for i := 0; i < 128; i++ {
		node := contextSample(now)
		node.NodeName, node.NodeUID = fmt.Sprintf("node-%d", i), fmt.Sprintf("uid-%d", i)
		for j := range rows {
			rows[j].NodeUID = node.NodeUID
		}
		batch, err := volumecontext.NewBatch(node.NodeName, node.NodeUID, now, volumecontext.SourceState(volumehealth.Reported, ""), rows, now)
		if err != nil {
			t.Fatal(err)
		}
		body, err := batch.EncodePrivate()
		if err != nil {
			t.Fatal(err)
		}
		before := store.volumes.bytes
		err = store.ReplaceNodeContextWithVolumes(node, body)
		if errors.Is(err, ErrStoreCapacity) {
			if accepted == 0 || store.volumes.bytes != before || store.volumes.bytes > limit {
				t.Fatal("capacity changed retained data")
			}
			record, _ := store.GetNodeContext(node.NodeName, now)
			if record.Report != nil {
				t.Fatal("capacity rejection advanced Node")
			}
			if err := store.ReconcileNodeIdentities(map[string]string{}, now); err != nil {
				t.Fatal(err)
			}
			if store.volumes.bytes != 0 {
				t.Fatal("removed Nodes did not release capacity")
			}
			return
		}
		if err != nil {
			t.Fatal(err)
		}
		accepted++
	}
	t.Fatal("retention failed to enforce the global byte limit")
}

func TestConcurrentVolumeReadsAndNodeReplacement(t *testing.T) {
	now := time.Now().UTC()
	store := NewStore()
	store.now = func() time.Time { return now }
	if err := store.ReconcileNodeIdentities(map[string]string{"node-a": "uid-a"}, now); err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceNodeContextWithVolumes(contextSample(now), volumeBody(t, now, "uid-a", 1)); err != nil {
		t.Fatal(err)
	}
	var readers sync.WaitGroup
	for i := 0; i < 8; i++ {
		readers.Go(func() {
			for j := 0; j < 20; j++ {
				data, err := store.VolumeSamples(volumeScope(now), now)
				if err != nil && !errors.Is(err, ErrNodeIdentityUnavailable) {
					t.Error(err)
				}
				if err != nil && (len(data.Current) > 0 || len(data.LastGood) > 0) {
					t.Error("replaced identity leaked evidence")
				}
			}
		})
	}
	if err := store.ReconcileNodeIdentities(map[string]string{"node-a": "new-uid"}, now); err != nil {
		t.Fatal(err)
	}
	readers.Wait()
	if store.volumes.bytes != 0 {
		t.Fatal("replacement retained data")
	}
}
