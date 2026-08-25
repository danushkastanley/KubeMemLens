package collector

import (
	"errors"
	"fmt"
	"net/url"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
)

func TestScopedViewsFilterBeforeAggregation(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	store := NewStore()
	_, err := store.ReplaceNodeSnapshot(api.AgentSnapshot{
		NodeName: "node-a", CapturedAt: now,
		Containers: []api.ContainerSnapshot{
			scopedContainer("team-a", "api", "uid-a", "container-a", now),
			scopedContainer("team-b", "api", "uid-b", "container-b", now),
		},
	})
	if err != nil {
		t.Fatalf("ReplaceNodeSnapshot: %v", err)
	}

	containers := store.ListContainersScoped(ReadScope{Namespace: "team-a"}, now, time.Minute)
	if len(containers) != 1 || containers[0].Namespace != "team-a" {
		t.Fatalf("scoped containers = %#v", containers)
	}
	pods := store.ListPodsScoped(ReadScope{Namespace: "team-a"}, now, time.Minute)
	if len(pods) != 1 || pods[0].Namespace != "team-a" || len(pods[0].Containers) != 1 {
		t.Fatalf("scoped pods = %#v", pods)
	}
	workloads := store.ListWorkloadsScoped(ReadScope{Namespace: "team-a"}, now, time.Minute)
	if len(workloads) != 1 || workloads[0].Namespace != "team-a" || len(workloads[0].Pods) != 1 {
		t.Fatalf("scoped workloads = %#v", workloads)
	}
	namespaces := store.ListNamespacesScoped(ReadScope{Namespace: "team-a"}, now, time.Minute)
	if len(namespaces) != 1 || namespaces[0].Namespace != "team-a" {
		t.Fatalf("scoped namespaces = %#v", namespaces)
	}

	pod, found := store.GetPod("team-a", "api", now, time.Minute)
	if !found || pod.Namespace != "team-a" || pod.PodUID != "uid-a" {
		t.Fatalf("GetPod found=%t pod=%#v", found, pod)
	}
	if _, found := store.GetPod("team-c", "api", now, time.Minute); found {
		t.Fatal("GetPod returned a Pod outside the requested namespace")
	}
}

func TestScopedContainerPaginationRejectsTokenFromAnotherScope(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	items := []api.ContainerSnapshot{
		scopedContainer("team-a", "api-a", "uid-a", "container-a", now),
		scopedContainer("team-a", "api-b", "uid-b", "container-b", now),
	}
	first, err := PaginateContainers(items, url.Values{"limit": {"1"}}, "containers:namespace:team-a")
	if err != nil {
		t.Fatalf("PaginateContainers: %v", err)
	}
	if len(first.Items) != 1 || first.Continue == "" {
		t.Fatalf("first page = %#v", first)
	}
	_, err = PaginateContainers(items, url.Values{"continue": {first.Continue}}, "containers:namespace:team-b")
	if err == nil || err.Error() != "continue token is invalid" {
		t.Fatalf("cross-scope token error = %v", err)
	}
	second, err := PaginateContainers(items, url.Values{"continue": {first.Continue}}, "containers:namespace:team-a")
	if err != nil || len(second.Items) != 1 {
		t.Fatalf("second page = %#v, err=%v", second, err)
	}
}

func TestScopedContainerPagesReturnEveryItemWithoutDuplicates(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	containers := make([]api.ContainerSnapshot, 1200)
	for index := range containers {
		containers[index] = scopedContainer("team-a", fmt.Sprintf("pod-%04d", index), fmt.Sprintf("uid-%04d", index), fmt.Sprintf("container-%04d", index), now)
	}
	store := NewStore()
	if _, err := store.ReplaceNodeSnapshot(api.AgentSnapshot{NodeName: "node-a", CapturedAt: now, Containers: containers}); err != nil {
		t.Fatalf("ReplaceNodeSnapshot: %v", err)
	}

	query := url.Values{"limit": {"500"}}
	seen := make(map[string]struct{}, len(containers))
	for {
		page, err := store.PageContainersScoped(ReadScope{Namespace: "team-a"}, now, time.Minute, query, "containers:namespace:team-a")
		if err != nil {
			t.Fatalf("PageContainersScoped: %v", err)
		}
		for _, item := range page.Items {
			if _, duplicate := seen[item.ContainerID]; duplicate {
				t.Fatalf("duplicate container %q", item.ContainerID)
			}
			seen[item.ContainerID] = struct{}{}
		}
		if page.Continue == "" {
			break
		}
		query.Set("continue", page.Continue)
	}
	if len(seen) != len(containers) {
		t.Fatalf("returned %d containers, want %d", len(seen), len(containers))
	}
}

func TestScopedAggregatePagesEnforceNestedBudget(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	store := NewStore()
	if _, err := store.ReplaceNodeSnapshot(api.AgentSnapshot{NodeName: "node-a", CapturedAt: now, Containers: []api.ContainerSnapshot{
		scopedContainer("team-a", "api", "uid-a", "container-a", now),
	}}); err != nil {
		t.Fatalf("ReplaceNodeSnapshot: %v", err)
	}
	query := url.Values{"limit": {"1"}}
	_, err := store.PagePodsScoped(ReadScope{Namespace: "team-a"}, now, time.Minute, query, "pods:namespace:team-a", 1)
	if !errors.Is(err, ErrReadPageTooLarge) {
		t.Fatalf("Pod page error = %v", err)
	}
	_, err = store.PageWorkloadsScoped(ReadScope{Namespace: "team-a"}, now, time.Minute, query, "workloads:namespace:team-a", 1)
	if !errors.Is(err, ErrReadPageTooLarge) {
		t.Fatalf("workload page error = %v", err)
	}
	_, _, err = store.GetPodBounded("team-a", "api", now, time.Minute, 1)
	if !errors.Is(err, ErrReadPageTooLarge) {
		t.Fatalf("direct Pod error = %v", err)
	}
}

func TestReadShardsRemainImmutableAcrossNodeReplacement(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	store := NewStore()
	first := scopedContainer("team-a", "first", "uid-a", "container-a", now)
	if _, err := store.ReplaceNodeSnapshot(api.AgentSnapshot{NodeName: "node-a", CapturedAt: now, Containers: []api.ContainerSnapshot{first}}); err != nil {
		t.Fatalf("ReplaceNodeSnapshot first: %v", err)
	}
	shards := store.readShards(now, time.Minute)
	second := scopedContainer("team-b", "second", "uid-b", "container-b", now.Add(time.Second))
	if _, err := store.ReplaceNodeSnapshot(api.AgentSnapshot{NodeName: "node-a", CapturedAt: now.Add(time.Second), Containers: []api.ContainerSnapshot{second}}); err != nil {
		t.Fatalf("ReplaceNodeSnapshot second: %v", err)
	}
	var names []string
	visitScopedShards(shards, ReadScope{}, func(container api.ContainerSnapshot) { names = append(names, container.PodName) })
	if len(names) != 1 || names[0] != "first" {
		t.Fatalf("captured shard changed after replacement: %v", names)
	}
}

func BenchmarkListContainersScopedAtConfiguredCapacity(b *testing.B) {
	now := time.Unix(1_800_000_000, 0).UTC()
	containers := make([]api.ContainerSnapshot, DefaultStoreLimits().MaxContainers)
	for index := range containers {
		namespace := "other"
		if index%100 == 0 {
			namespace = "target"
		}
		containers[index] = api.ContainerSnapshot{
			Namespace: namespace, PodName: "pod", PodUID: "uid", ContainerName: "app",
			ContainerID: string(rune(index + 1)), CapturedAt: now,
		}
	}
	store := NewStore()
	if _, err := store.ReplaceNodeSnapshot(api.AgentSnapshot{NodeName: "node-a", CapturedAt: now, Containers: containers}); err != nil {
		b.Fatalf("ReplaceNodeSnapshot: %v", err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		items := store.ListContainersScoped(ReadScope{Namespace: "target"}, now, time.Minute)
		if len(items) != 1000 {
			b.Fatalf("items=%d", len(items))
		}
	}
}

func BenchmarkContainerPageAtConfiguredCapacity(b *testing.B) {
	now := time.Unix(1_800_000_000, 0).UTC()
	containers := make([]api.ContainerSnapshot, DefaultStoreLimits().MaxContainers)
	for index := range containers {
		containers[index] = api.ContainerSnapshot{
			Namespace: "target", PodName: fmt.Sprintf("pod-%06d", index), PodUID: fmt.Sprintf("uid-%06d", index),
			ContainerName: "app", ContainerID: fmt.Sprintf("container-%06d", index), CapturedAt: now,
		}
	}
	store := NewStore()
	if _, err := store.ReplaceNodeSnapshot(api.AgentSnapshot{NodeName: "node-a", CapturedAt: now, Containers: containers}); err != nil {
		b.Fatalf("ReplaceNodeSnapshot: %v", err)
	}
	query := url.Values{"limit": {"500"}}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		page, err := store.PageContainersScoped(ReadScope{Namespace: "target"}, now, time.Minute, query, "containers:namespace:target")
		if err != nil || len(page.Items) != 500 || page.Continue == "" {
			b.Fatalf("page items=%d continue=%t err=%v", len(page.Items), page.Continue != "", err)
		}
	}
}

func BenchmarkContainerPageScopedAtConfiguredCapacity(b *testing.B) {
	now := time.Unix(1_800_000_000, 0).UTC()
	containers := make([]api.ContainerSnapshot, DefaultStoreLimits().MaxContainers)
	for index := range containers {
		namespace := "other"
		if index%100 == 0 {
			namespace = "target"
		}
		containers[index] = api.ContainerSnapshot{
			Namespace: namespace, PodName: fmt.Sprintf("pod-%06d", index), PodUID: fmt.Sprintf("uid-%06d", index),
			ContainerName: "app", ContainerID: fmt.Sprintf("container-%06d", index), CapturedAt: now,
		}
	}
	store := NewStore()
	if _, err := store.ReplaceNodeSnapshot(api.AgentSnapshot{NodeName: "node-a", CapturedAt: now, Containers: containers}); err != nil {
		b.Fatalf("ReplaceNodeSnapshot: %v", err)
	}
	query := url.Values{"limit": {"500"}}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		page, err := store.PageContainersScoped(ReadScope{Namespace: "target"}, now, time.Minute, query, "containers:namespace:target")
		if err != nil || len(page.Items) != 500 || page.Continue == "" {
			b.Fatalf("page items=%d continue=%t err=%v", len(page.Items), page.Continue != "", err)
		}
	}
}

func scopedContainer(namespace, pod, podUID, containerID string, capturedAt time.Time) api.ContainerSnapshot {
	return api.ContainerSnapshot{
		Namespace: namespace, PodName: pod, PodUID: podUID, ContainerName: "app", ContainerID: containerID,
		NodeName: "node-a", CapturedAt: capturedAt,
		Context: api.ContainerContext{WorkloadKind: "Deployment", WorkloadName: "api"},
	}
}
