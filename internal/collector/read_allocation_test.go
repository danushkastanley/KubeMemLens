package collector

import (
	"fmt"
	"runtime"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
)

const (
	allocationNodes             = 100
	allocationContainersPerNode = 1000
)

// fullStore holds the default ceiling of 100,000 containers: 100 nodes, 50
// namespaces and two containers per Pod.
func fullStore(t testing.TB, capturedAt time.Time) *Store {
	t.Helper()
	store := NewStore()
	for node := 0; node < allocationNodes; node++ {
		containers := make([]api.ContainerSnapshot, allocationContainersPerNode)
		for index := range containers {
			item := scaleContainer(node*allocationContainersPerNode+index, capturedAt)
			item.Namespace = fmt.Sprintf("team-%02d", index%50)
			item.PodName = fmt.Sprintf("worker-%03d-%03d", node, index/2)
			item.PodUID = "uid-" + item.PodName
			item.ContainerName = fmt.Sprintf("c%d", index%2)
			containers[index] = item
		}
		snapshot := api.AgentSnapshot{NodeName: fmt.Sprintf("node-%03d", node), CapturedAt: capturedAt, Containers: containers}
		if _, err := store.ReplaceNodeSnapshot(snapshot); err != nil {
			t.Fatalf("fill store: %v", err)
		}
	}
	return store
}

func allocatedBytes(read func()) uint64 {
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	read()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

func TestReadsOfAFullStoreDoNotCopyEveryContainer(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping 100,000-container allocation check in short mode")
	}
	now := time.Now().UTC()
	store := fullStore(t, now)
	const mebibyte = 1 << 20
	for name, test := range map[string]struct {
		limit uint64
		read  func()
	}{
		"single Pod": {mebibyte, func() {
			if _, found, err := store.GetPodBounded("team-07", "worker-042-003", now, time.Minute, mebibyte); err != nil || !found {
				t.Errorf("single Pod read: found=%v err=%v", found, err)
			}
		}},
		"cluster status": {16 * mebibyte, func() {
			if debug := store.Debug(now, time.Minute); debug.TotalContainers != allocationNodes*allocationContainersPerNode {
				t.Errorf("status counted %d containers", debug.TotalContainers)
			}
		}},
		"workload volume Pods": {mebibyte, func() {
			pods, err := store.WorkloadVolumePods("team-07", map[string]string{"worker-042-003": "uid-worker-042-003"}, now, time.Minute)
			if err != nil || len(pods) != 1 {
				t.Errorf("workload volume read: pods=%d err=%v", len(pods), err)
			}
		}},
	} {
		t.Run(name, func(t *testing.T) {
			if allocated := allocatedBytes(test.read); allocated > test.limit {
				t.Fatalf("read allocated %.1f MiB, want at most %d MiB", float64(allocated)/mebibyte, test.limit/mebibyte)
			} else {
				t.Logf("read allocated %.2f MiB", float64(allocated)/mebibyte)
			}
		})
	}
}

func TestSharedShardsStillStampFreshnessAndCompleteness(t *testing.T) {
	captured := time.Now().UTC().Add(-10 * time.Minute)
	store := NewStore()
	container := scaleContainer(1, captured)
	// A value posted by an agent must never reach a reader.
	container.Freshness, container.Completeness = api.EvidenceFreshnessFresh, api.EvidencePartial
	snapshot := api.AgentSnapshot{NodeName: "node-a", CapturedAt: captured, Containers: []api.ContainerSnapshot{container},
		Environment: api.NodeEnvironment{NodeContextAvailable: true, WorkloadContextAvailable: true}}
	if _, err := store.ReplaceNodeSnapshot(snapshot); err != nil {
		t.Fatal(err)
	}
	for name, test := range map[string]struct {
		now  time.Time
		want api.EvidenceFreshness
	}{
		"within the lifetime": {captured.Add(30 * time.Second), api.EvidenceFreshnessFresh},
		"after the lifetime":  {captured.Add(10 * time.Minute), api.EvidenceFreshnessStale},
	} {
		t.Run(name, func(t *testing.T) {
			pod, found, err := store.GetPodBounded(container.Namespace, container.PodName, test.now, time.Minute, 1<<20)
			if err != nil || !found {
				t.Fatalf("found=%v err=%v", found, err)
			}
			listed := store.ListContainers(test.now, time.Minute)
			scoped := store.ListContainersScoped(ReadScope{Namespace: container.Namespace}, test.now, time.Minute)
			for source, got := range map[string]api.ContainerSnapshot{"pod": pod.Containers[0], "list": listed[0], "scoped list": scoped[0]} {
				if got.Freshness != test.want || got.Completeness != api.EvidenceComplete {
					t.Fatalf("%s: freshness=%s completeness=%s, want %s/complete", source, got.Freshness, got.Completeness, test.want)
				}
			}
		})
	}
	// The second read used a later clock; the first result must not have changed underneath.
	early := store.ListContainers(captured.Add(30*time.Second), time.Minute)
	_ = store.ListContainers(captured.Add(10*time.Minute), time.Minute)
	if early[0].Freshness != api.EvidenceFreshnessFresh {
		t.Fatalf("a later read rewrote an earlier result: %s", early[0].Freshness)
	}
}

// Readers keep the store's slices after the lock is released, so this runs
// under the race detector with a writer replacing the node they are reading.
func TestReadsShareShardsSafelyWithConcurrentReplacement(t *testing.T) {
	start := time.Now().UTC()
	store := NewStore()
	replace := func(round int) {
		containers := make([]api.ContainerSnapshot, 200)
		for index := range containers {
			containers[index] = scaleContainer(index, start)
		}
		snapshot := api.AgentSnapshot{NodeName: "node-a", CapturedAt: start.Add(time.Duration(round) * time.Second), Containers: containers}
		if _, err := store.ReplaceNodeSnapshot(snapshot); err != nil {
			t.Errorf("replace: %v", err)
		}
	}
	replace(0)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for round := 1; round <= 200; round++ {
			replace(round)
		}
	}()
	for running := true; running; {
		select {
		case <-done:
			running = false
		default:
		}
		now := start.Add(time.Minute)
		if got := len(store.ListContainers(now, time.Hour)); got != 200 {
			t.Fatalf("list returned %d containers during replacement", got)
		}
		if debug := store.Debug(now, time.Hour); debug.TotalContainers != 200 {
			t.Fatalf("status counted %d containers during replacement", debug.TotalContainers)
		}
		if _, found, err := store.GetPodBounded("memlens-density", "memory-worker-00007", now, time.Hour, 1<<20); err != nil || !found {
			t.Fatalf("Pod read during replacement: found=%v err=%v", found, err)
		}
	}
}
