package collector

import (
	"sync"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
)

func TestNodeContextConcurrentReadersCannotMutateStoredEvidence(t *testing.T) {
	now := time.Now().UTC()
	store := NewStore()
	store.now = func() time.Time { return now }
	if err := store.ReconcileNodeIdentities(map[string]string{"node-a": "uid-a"}, now); err != nil {
		t.Fatal(err)
	}
	var group sync.WaitGroup
	group.Add(2)
	go func() {
		defer group.Done()
		for i := 0; i < 100; i++ {
			if err := store.ReplaceNodeContext(contextSample(now.Add(time.Duration(i) * time.Millisecond))); err != nil {
				t.Error(err)
				return
			}
		}
	}()
	go func() {
		defer group.Done()
		for i := 0; i < 100; i++ {
			_, err := store.ReplaceNodeSnapshot(api.AgentSnapshot{NodeName: "node-a", CapturedAt: now.Add(time.Duration(i) * time.Millisecond), Containers: []api.ContainerSnapshot{{ContainerID: "app"}}})
			if err != nil {
				t.Error(err)
				return
			}
		}
	}()
	for range 4 {
		group.Add(1)
		go func() {
			defer group.Done()
			for range 100 {
				value, _ := store.GetNodeContext("node-a", now)
				if value.LastGood != nil {
					*value.LastGood.Stats.Memory.UsageBytes = 456
				}
				if _, err := store.PageNodeContextHistory("node-a", now, nil, 8<<20); err != nil {
					t.Error(err)
					return
				}
				_ = store.NodeContextDebug(now)
			}
		}()
	}
	group.Wait()
	value, _ := store.GetNodeContext("node-a", now)
	if *value.LastGood.Stats.Memory.UsageBytes != 123 {
		t.Fatal("reader changed retained evidence")
	}
	if len(store.ListContainers(now, time.Minute)) != 1 {
		t.Fatal("Node producer lost cgroup records")
	}
}
