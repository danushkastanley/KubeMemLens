package extension

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/collector"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestCoordinatorIdempotencyAndReplay(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	coordinator := testCoordinator(t, collector.NewStore(), now, 10)
	claims := testClaims("pod-a", "node-a", "node-uid-a")
	request := testRequest(now, "epoch-a", 7, "node-a", "node-uid-a")

	first, duplicate, err := coordinator.Accept(claims, request)
	if err != nil || duplicate || !first.Accepted {
		t.Fatalf("first Accept = %#v, duplicate=%t, err=%v", first, duplicate, err)
	}
	second, duplicate, err := coordinator.Accept(claims, request)
	if err != nil || !duplicate || !second.Duplicate || second.Containers != first.Containers {
		t.Fatalf("duplicate Accept = %#v, duplicate=%t, err=%v", second, duplicate, err)
	}

	changed := request
	changed.Snapshot.Environment.CgroupDriver = "systemd"
	_, _, err = coordinator.Accept(claims, changed)
	assertIngestionCode(t, err, "sequence_conflict")
	lower := request
	lower.Sequence = 6
	_, _, err = coordinator.Accept(claims, lower)
	assertIngestionCode(t, err, "sequence_replayed")

	higher := request
	higher.Sequence = 8
	higher.Snapshot.CapturedAt = now.Add(time.Second)
	rotated := claims
	rotated.CredentialID = "credential-b"
	if _, _, err := coordinator.Accept(rotated, higher); err != nil {
		t.Fatalf("credential rotation rejected: %v", err)
	}
}

func TestCoordinatorRejectsEpochAndNodeMismatchWithoutAdvancing(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	coordinator := testCoordinator(t, collector.NewStore(), now, 10)
	claims := testClaims("pod-a", "node-a", "node-uid-a")

	wrongEpoch := testRequest(now, "old-epoch", 1, "node-a", "node-uid-a")
	_, _, err := coordinator.Accept(claims, wrongEpoch)
	assertIngestionCode(t, err, "epoch_mismatch")
	wrongNode := testRequest(now, "epoch-a", 1, "node-b", "node-uid-b")
	_, _, err = coordinator.Accept(claims, wrongNode)
	assertIngestionCode(t, err, "node_claim_mismatch")
	valid := testRequest(now, "epoch-a", 1, "node-a", "node-uid-a")
	if _, _, err := coordinator.Accept(claims, valid); err != nil {
		t.Fatalf("valid request after rejection failed: %v", err)
	}
}

func TestCoordinatorReplacedPodCannotReclaimNode(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	coordinator := testCoordinator(t, collector.NewStore(), now, 10)
	oldClaims := testClaims("pod-old", "node-a", "node-uid-a")
	newClaims := testClaims("pod-new", "node-a", "node-uid-a")
	if _, _, err := coordinator.Accept(oldClaims, testRequest(now, "epoch-a", 4, "node-a", "node-uid-a")); err != nil {
		t.Fatalf("old owner first request failed: %v", err)
	}
	replacement := testRequest(now.Add(time.Second), "epoch-a", 1, "node-a", "node-uid-a")
	if _, _, err := coordinator.Accept(newClaims, replacement); err != nil {
		t.Fatalf("replacement request failed: %v", err)
	}
	oldRetry := testRequest(now.Add(2*time.Second), "epoch-a", 5, "node-a", "node-uid-a")
	_, _, err := coordinator.Accept(oldClaims, oldRetry)
	assertIngestionCode(t, err, "agent_replaced")
}

func TestCoordinatorRetiredIdentityExpiresWithoutBlockingLaterRollout(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	store := collector.NewStore()
	coordinator, err := NewCoordinator(store, CoordinatorOptions{
		Handler: collector.DefaultHandlerOptions(time.Minute), MaxAgents: 2, MaxRetired: 1,
		RetiredTTL: 2 * time.Minute, Now: func() time.Time { return now }, Epoch: "epoch-a",
	})
	if err != nil {
		t.Fatalf("NewCoordinator: %v", err)
	}
	first := testClaims("pod-first", "node-a", "node-uid-a")
	second := testClaims("pod-second", "node-a", "node-uid-a")
	third := testClaims("pod-third", "node-a", "node-uid-a")
	if _, _, err := coordinator.Accept(first, testRequest(now, "epoch-a", 1, "node-a", "node-uid-a")); err != nil {
		t.Fatalf("first rollout: %v", err)
	}
	if _, _, err := coordinator.Accept(second, testRequest(now.Add(time.Second), "epoch-a", 1, "node-a", "node-uid-a")); err != nil {
		t.Fatalf("second rollout: %v", err)
	}
	now = now.Add(3 * time.Minute)
	if _, _, err := coordinator.Accept(third, testRequest(now, "epoch-a", 1, "node-a", "node-uid-a")); err != nil {
		t.Fatalf("rollout after retired identity expiry: %v", err)
	}
}

func TestCoordinatorStoreFailureDoesNotAdvanceSequence(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	store := collector.NewStoreWithHistoryAndLimits(collector.DefaultHistoryOptions(), collector.StoreLimits{MaxNodes: 1, MaxContainers: 1})
	coordinator := testCoordinator(t, store, now, 10)
	claims := testClaims("pod-a", "node-a", "node-uid-a")
	tooLarge := testRequest(now, "epoch-a", 1, "node-a", "node-uid-a")
	tooLarge.Snapshot.Containers = append(tooLarge.Snapshot.Containers, tooLarge.Snapshot.Containers[0])
	tooLarge.Snapshot.Containers[1].ContainerID = "container-b"
	_, _, err := coordinator.Accept(claims, tooLarge)
	assertIngestionCode(t, err, "store_capacity")
	if _, _, err := coordinator.Accept(claims, testRequest(now, "epoch-a", 1, "node-a", "node-uid-a")); err != nil {
		t.Fatalf("corrected request at same sequence failed: %v", err)
	}
}

func TestCoordinatorConcurrentDuplicateMutatesOnce(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	store := collector.NewStore()
	coordinator := testCoordinator(t, store, now, 10)
	claims := testClaims("pod-a", "node-a", "node-uid-a")
	request := testRequest(now, "epoch-a", 1, "node-a", "node-uid-a")
	var wg sync.WaitGroup
	duplicates := 0
	var mu sync.Mutex
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, duplicate, err := coordinator.Accept(claims, request)
			if err != nil {
				t.Errorf("Accept returned error: %v", err)
			}
			if duplicate {
				mu.Lock()
				duplicates++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if duplicates != 7 {
		t.Fatalf("duplicates = %d, want 7", duplicates)
	}
	history := store.ListPodHistory("default", "pod-a", "", now)
	if len(history) != 1 || len(history[0].Points) != 1 {
		t.Fatalf("duplicate mutated history: %#v", history)
	}
}

func testCoordinator(t *testing.T, store *collector.Store, now time.Time, maxAgents int) *Coordinator {
	t.Helper()
	coordinator, err := NewCoordinator(store, CoordinatorOptions{
		Handler: collector.DefaultHandlerOptions(time.Minute), MaxAgents: maxAgents,
		MaxRetired: maxAgents * 4, Now: func() time.Time { return now }, Epoch: "epoch-a",
	})
	if err != nil {
		t.Fatalf("NewCoordinator: %v", err)
	}
	return coordinator
}

func testClaims(podUID, nodeName, nodeUID string) AgentClaims {
	return AgentClaims{PodUID: podUID, NodeName: nodeName, NodeUID: nodeUID, CredentialID: "credential-a"}
}

func testRequest(capturedAt time.Time, epoch string, sequence uint64, nodeName, nodeUID string) api.NodeSnapshotRequest {
	return api.NodeSnapshotRequest{
		TypeMeta: metav1.TypeMeta{APIVersion: api.MemoryAPIGroup + "/" + api.MemoryAPIVersion, Kind: "NodeSnapshot"},
		NodeUID:  nodeUID, Epoch: epoch, Sequence: sequence,
		Snapshot: api.AgentSnapshot{
			SchemaVersion: api.CurrentSnapshotSchemaVersion, NodeName: nodeName, CapturedAt: capturedAt,
			Containers: []api.ContainerSnapshot{{
				Namespace: "default", PodName: "pod-a", PodUID: "workload-pod-uid", ContainerName: "app",
				ContainerID: "container-a", NodeName: nodeName, CapturedAt: capturedAt,
			}},
		},
	}
}

func assertIngestionCode(t *testing.T, err error, want string) {
	t.Helper()
	var ingestionErr *IngestionError
	if !errors.As(err, &ingestionErr) || ingestionErr.Code != want {
		t.Fatalf("error = %v, want ingestion code %q", err, want)
	}
}
