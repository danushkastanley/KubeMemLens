package collector

import (
	"bytes"
	"container/heap"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"sort"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/aggregate"
	"github.com/danushkastanley/kube-memlens/internal/api"
)

var ErrReadPageTooLarge = errors.New("authorised page exceeds the configured response work limit")

type ScopedPodPage struct {
	Items    []api.PodSnapshot
	Continue string
}

type ScopedWorkloadPage struct {
	Items    []api.WorkloadSnapshot
	Continue string
}

func (s *Store) PageContainersScoped(scope ReadScope, now time.Time, ttl time.Duration, query url.Values, tokenScope string) (api.ContainerPage, error) {
	selection, err := newDigestSelection(query, tokenScope)
	if err != nil {
		return api.ContainerPage{}, err
	}
	shards := s.readShards(now, ttl)
	hasher := identityBuffer{}
	visitScopedShards(shards, scope, func(container api.ContainerSnapshot) {
		selection.add(hasher.container(container))
	})
	keys, continuation := selection.page(tokenScope)
	selected := make(map[digestKey]int, len(keys))
	for index, key := range keys {
		selected[key] = index
	}
	items := make([]api.ContainerSnapshot, len(keys))
	visitScopedShards(shards, scope, func(container api.ContainerSnapshot) {
		if index, ok := selected[hasher.container(container)]; ok {
			items[index] = container
		}
	})
	return api.ContainerPage{Items: items, Continue: continuation}, nil
}

func (s *Store) PagePodsScoped(scope ReadScope, now time.Time, ttl time.Duration, query url.Values, tokenScope string, maxNestedBytes int) (ScopedPodPage, error) {
	selection, err := newDigestSelection(query, tokenScope)
	if err != nil {
		return ScopedPodPage{}, err
	}
	shards := s.readShards(now, ttl)
	hasher := identityBuffer{}
	visitScopedShards(shards, scope, func(container api.ContainerSnapshot) {
		if key, ok := hasher.pod(container); ok {
			selection.add(key)
		}
	})
	keys, continuation := selection.page(tokenScope)
	containers, err := containersForSelection(shards, scope, keys, (*identityBuffer).pod, maxNestedBytes)
	if err != nil {
		return ScopedPodPage{}, err
	}
	items := orderPodsByDigest(aggregate.Pods(containers), keys)
	return ScopedPodPage{Items: items, Continue: continuation}, nil
}

func (s *Store) PageWorkloadsScoped(scope ReadScope, now time.Time, ttl time.Duration, query url.Values, tokenScope string, maxNestedBytes int) (ScopedWorkloadPage, error) {
	selection, err := newDigestSelection(query, tokenScope)
	if err != nil {
		return ScopedWorkloadPage{}, err
	}
	shards := s.readShards(now, ttl)
	hasher := identityBuffer{}
	visitScopedShards(shards, scope, func(container api.ContainerSnapshot) {
		if key, ok := hasher.workload(container); ok {
			selection.add(key)
		}
	})
	keys, continuation := selection.page(tokenScope)
	containers, err := containersForSelection(shards, scope, keys, (*identityBuffer).workload, maxNestedBytes)
	if err != nil {
		return ScopedWorkloadPage{}, err
	}
	items := aggregate.Workloads(aggregate.Pods(containers))
	return ScopedWorkloadPage{Items: orderWorkloadsByDigest(items, keys), Continue: continuation}, nil
}

func (s *Store) GetPodBounded(namespace, name string, now time.Time, ttl time.Duration, maxNestedBytes int) (api.PodSnapshot, bool, error) {
	shards := s.readShards(now, ttl)
	containers := make([]api.ContainerSnapshot, 0)
	budget := newJSONBudget(maxNestedBytes)
	var budgetErr error
	visitScopedShards(shards, ReadScope{Namespace: namespace}, func(container api.ContainerSnapshot) {
		if container.PodName == name && budgetErr == nil {
			budgetErr = budget.add(container)
			if budgetErr == nil {
				containers = append(containers, container)
			}
		}
	})
	if budgetErr != nil {
		return api.PodSnapshot{}, false, budgetErr
	}
	pods := aggregate.Pods(containers)
	if len(pods) == 0 {
		return api.PodSnapshot{}, false, nil
	}
	latest := pods[0]
	for _, pod := range pods[1:] {
		if pod.CapturedAt.After(latest.CapturedAt) ||
			(pod.CapturedAt.Equal(latest.CapturedAt) && pod.PodUID < latest.PodUID) {
			latest = pod
		}
	}
	return latest, true, nil
}

type containerDigester func(*identityBuffer, api.ContainerSnapshot) (digestKey, bool)

func containersForSelection(shards [][]api.ContainerSnapshot, scope ReadScope, keys []digestKey, digester containerDigester, maxNestedBytes int) ([]api.ContainerSnapshot, error) {
	selected := make(map[digestKey]struct{}, len(keys))
	for _, key := range keys {
		selected[key] = struct{}{}
	}
	containers := make([]api.ContainerSnapshot, 0)
	budget := newJSONBudget(maxNestedBytes)
	hasher := identityBuffer{}
	var budgetErr error
	visitScopedShards(shards, scope, func(container api.ContainerSnapshot) {
		key, valid := digester(&hasher, container)
		if _, ok := selected[key]; valid && ok && budgetErr == nil {
			budgetErr = budget.add(container)
			if budgetErr == nil {
				containers = append(containers, container)
			}
		}
	})
	if budgetErr != nil {
		return nil, budgetErr
	}
	return containers, nil
}

func (s *Store) pruneStaleLocked(now time.Time, ttl time.Duration) {
	for node, snapshot := range s.nodes {
		if isStale(snapshot.capturedAt, now, ttl) {
			s.containerCount -= len(snapshot.containers)
			snapshot.containers = nil
			s.nodes[node] = snapshot
		}
	}
}

func (s *Store) readShards(now time.Time, ttl time.Duration) [][]api.ContainerSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneStaleLocked(now, ttl)
	shards := make([][]api.ContainerSnapshot, 0, len(s.nodes))
	for _, snapshot := range s.nodes {
		if len(snapshot.containers) > 0 {
			shards = append(shards, snapshot.containers)
		}
	}
	return shards
}

func visitScopedShards(shards [][]api.ContainerSnapshot, scope ReadScope, visit func(api.ContainerSnapshot)) {
	for _, shard := range shards {
		for _, container := range shard {
			if scope.Namespace == "" || container.Namespace == scope.Namespace {
				visit(container)
			}
		}
	}
}

type jsonBudget struct {
	remaining int
}

func newJSONBudget(maxBytes int) *jsonBudget {
	return &jsonBudget{remaining: maxBytes}
}

func (b *jsonBudget) add(value any) error {
	if b.remaining <= 0 {
		return ErrReadPageTooLarge
	}
	writer := &limitedCountWriter{remaining: b.remaining}
	if err := json.NewEncoder(writer).Encode(value); err != nil {
		return ErrReadPageTooLarge
	}
	b.remaining = writer.remaining
	return nil
}

type limitedCountWriter struct {
	remaining int
}

func (w *limitedCountWriter) Write(data []byte) (int, error) {
	if len(data) > w.remaining {
		return 0, ErrReadPageTooLarge
	}
	w.remaining -= len(data)
	return len(data), nil
}

type digestSelection struct {
	after    digestKey
	limit    int
	selected maxDigestHeap
	seen     map[digestKey]struct{}
}

func newDigestSelection(query url.Values, scope string) (*digestSelection, error) {
	limit, err := pageLimit(query)
	if err != nil {
		return nil, err
	}
	after, err := decodeScopedContainerCursor(query.Get("continue"), scope)
	if err != nil {
		return nil, err
	}
	var afterKey digestKey
	if after != "" {
		_, _ = hex.Decode(afterKey[:], []byte(after))
	}
	return &digestSelection{after: afterKey, limit: limit, seen: make(map[digestKey]struct{}, limit+1)}, nil
}

func (s *digestSelection) add(key digestKey) {
	if compareDigest(key, s.after) <= 0 {
		return
	}
	if _, exists := s.seen[key]; exists {
		return
	}
	if len(s.selected) < s.limit+1 {
		heap.Push(&s.selected, key)
		s.seen[key] = struct{}{}
		return
	}
	if compareDigest(key, s.selected[0]) >= 0 {
		return
	}
	delete(s.seen, heap.Pop(&s.selected).(digestKey))
	heap.Push(&s.selected, key)
	s.seen[key] = struct{}{}
}

func (s *digestSelection) page(scope string) ([]digestKey, string) {
	keys := append([]digestKey(nil), s.selected...)
	sort.Slice(keys, func(i, j int) bool { return compareDigest(keys[i], keys[j]) < 0 })
	hasMore := len(keys) > s.limit
	if hasMore {
		keys = keys[:s.limit]
	}
	if hasMore && len(keys) > 0 {
		return keys, encodeScopedContainerCursor(scope, hex.EncodeToString(keys[len(keys)-1][:]))
	}
	return keys, ""
}

type digestKey [sha256.Size]byte

type maxDigestHeap []digestKey

func (h maxDigestHeap) Len() int           { return len(h) }
func (h maxDigestHeap) Less(i, j int) bool { return compareDigest(h[i], h[j]) > 0 }
func (h maxDigestHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *maxDigestHeap) Push(value any)    { *h = append(*h, value.(digestKey)) }
func (h *maxDigestHeap) Pop() any {
	items := *h
	last := items[len(items)-1]
	*h = items[:len(items)-1]
	return last
}

func compareDigest(left, right digestKey) int {
	return bytes.Compare(left[:], right[:])
}

type identityBuffer struct {
	data []byte
}

func (b *identityBuffer) digest(parts ...string) digestKey {
	b.data = b.data[:0]
	for index, part := range parts {
		if index > 0 {
			b.data = append(b.data, 0)
		}
		b.data = append(b.data, part...)
	}
	return sha256.Sum256(b.data)
}

func (b *identityBuffer) container(item api.ContainerSnapshot) digestKey {
	return b.digest(item.Namespace, item.PodUID, item.PodName, item.ContainerName, item.ContainerID, item.NodeName)
}

func (b *identityBuffer) pod(item api.ContainerSnapshot) (digestKey, bool) {
	if item.Namespace == "" || item.PodName == "" {
		return digestKey{}, false
	}
	return b.digest(item.Namespace, item.PodUID, item.PodName, item.NodeName), true
}

func (b *identityBuffer) workload(item api.ContainerSnapshot) (digestKey, bool) {
	if item.Namespace == "" || item.PodName == "" {
		return digestKey{}, false
	}
	kind, name := item.Context.WorkloadKind, item.Context.WorkloadName
	if kind == "" || name == "" {
		kind, name = item.Context.OwnerKind, item.Context.OwnerName
	}
	if kind == "" || name == "" {
		kind, name = "Pod", item.PodName
	}
	return b.digest(item.Namespace, kind, name), true
}

func orderPodsByDigest(items []api.PodSnapshot, keys []digestKey) []api.PodSnapshot {
	byKey := make(map[digestKey]api.PodSnapshot, len(items))
	hasher := identityBuffer{}
	for _, item := range items {
		byKey[hasher.digest(item.Namespace, item.PodUID, item.PodName, item.NodeName)] = item
	}
	ordered := make([]api.PodSnapshot, 0, len(items))
	for _, key := range keys {
		if item, ok := byKey[key]; ok {
			ordered = append(ordered, item)
		}
	}
	return ordered
}

func orderWorkloadsByDigest(items []api.WorkloadSnapshot, keys []digestKey) []api.WorkloadSnapshot {
	byKey := make(map[digestKey]api.WorkloadSnapshot, len(items))
	hasher := identityBuffer{}
	for _, item := range items {
		byKey[hasher.digest(item.Namespace, item.Kind, item.Name)] = item
	}
	ordered := make([]api.WorkloadSnapshot, 0, len(items))
	for _, key := range keys {
		if item, ok := byKey[key]; ok {
			ordered = append(ordered, item)
		}
	}
	return ordered
}
