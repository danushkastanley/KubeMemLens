package collector

import (
	"fmt"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
)

const (
	concurrentNodes        = 6
	concurrentPodsPerNode  = 5
	concurrentNamespaces   = 3
	concurrentTotal        = concurrentNodes * concurrentPodsPerNode
	concurrentPerNamespace = concurrentTotal / concurrentNamespaces
)

func concurrentSnapshot(node int, capturedAt time.Time) api.AgentSnapshot {
	snapshot := api.AgentSnapshot{SchemaVersion: api.CurrentSnapshotSchemaVersion, NodeName: fmt.Sprintf("node-%d", node), CapturedAt: capturedAt}
	for pod := 0; pod < concurrentPodsPerNode; pod++ {
		index := node*concurrentPodsPerNode + pod
		snapshot.Containers = append(snapshot.Containers, api.ContainerSnapshot{
			Namespace: fmt.Sprintf("team-%d", index%concurrentNamespaces), PodName: fmt.Sprintf("pod-%d", index), PodUID: fmt.Sprintf("uid-%d", index),
			ContainerName: "app", ContainerID: fmt.Sprintf("container-%d", index),
		})
	}
	return snapshot
}

// pageEverything follows continue tokens to the end and returns how often each
// container was seen.
func pageEverything(store *Store, scope ReadScope, now time.Time) (map[string]int, error) {
	seen := map[string]int{}
	query := url.Values{"limit": {"4"}}
	tokenScope := "containers:" + scope.Namespace + ":" + store.Generation()
	for {
		page, err := store.PageContainersScoped(scope, now, time.Hour, query, tokenScope)
		if err != nil {
			return nil, err
		}
		for _, item := range page.Items {
			if scope.Namespace != "" && item.Namespace != scope.Namespace {
				return nil, fmt.Errorf("namespace %q appeared in a read scoped to %q", item.Namespace, scope.Namespace)
			}
			seen[item.ContainerID]++
		}
		if page.Continue == "" {
			return seen, nil
		}
		query.Set("continue", page.Continue)
	}
}

// Agents replace their node's snapshot every few seconds while users page
// through lists. The same containers are posted again and again here, so
// every complete listing must return each container in its scope exactly
// once, whatever is being replaced at that moment. Run with -race.
func TestPagedReadsStayCompleteWhileSnapshotsAreReplaced(t *testing.T) {
	start := time.Now().UTC()
	store := NewStore()
	for node := 0; node < concurrentNodes; node++ {
		if _, err := store.ReplaceNodeSnapshot(concurrentSnapshot(node, start)); err != nil {
			t.Fatal(err)
		}
	}

	var writers sync.WaitGroup
	stop := make(chan struct{})
	for node := 0; node < concurrentNodes; node++ {
		writers.Go(func() {
			// Keep replacing until every reader has finished.
			for round := 1; ; round++ {
				select {
				case <-stop:
					return
				default:
				}
				if _, err := store.ReplaceNodeSnapshot(concurrentSnapshot(node, start.Add(time.Duration(round)*time.Millisecond))); err != nil {
					t.Errorf("replace node %d round %d: %v", node, round, err)
					return
				}
			}
		})
	}

	var readers sync.WaitGroup
	for _, scope := range []ReadScope{{}, {Namespace: "team-0"}, {Namespace: "team-2"}} {
		want := concurrentTotal
		if scope.Namespace != "" {
			want = concurrentPerNamespace
		}
		readers.Go(func() {
			for pass := 0; pass < 40; pass++ {
				seen, err := pageEverything(store, scope, start.Add(time.Second))
				if err != nil {
					t.Errorf("scope %q pass %d: %v", scope.Namespace, pass, err)
					return
				}
				if len(seen) != want {
					t.Errorf("scope %q pass %d: %d containers listed, want %d", scope.Namespace, pass, len(seen), want)
					return
				}
				for id, count := range seen {
					if count != 1 {
						t.Errorf("scope %q pass %d: container %s listed %d times", scope.Namespace, pass, id, count)
						return
					}
				}
			}
		})
	}
	readers.Wait()
	close(stop)
	writers.Wait()
}
