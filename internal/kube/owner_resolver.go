package kube

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
)

const defaultOwnerCacheEntries = 2000

var ErrOwnerReplaced = errors.New("workload owner identity changed")
var ErrOwnerAPI = errors.New("workload owner API is unsupported")

type ownerCacheEntry struct {
	kind      string
	name      string
	loadedAt  time.Time
	expiresAt time.Time
}

// WorkloadOwnerResolver follows only the Kubernetes controller hops needed to
// turn ReplicaSet and Job owners into top-level Deployment and CronJob owners.
// Successful lookups are cached to avoid adding API traffic to every scan.
type WorkloadOwnerResolver struct {
	client     kubernetes.Interface
	ttl        time.Duration
	maxEntries int

	mu      sync.Mutex
	entries map[string]ownerCacheEntry
}

func NewWorkloadOwnerResolver(client kubernetes.Interface, ttl time.Duration) *WorkloadOwnerResolver {
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	return &WorkloadOwnerResolver{
		client: client, ttl: ttl, maxEntries: defaultOwnerCacheEntries,
		entries: map[string]ownerCacheEntry{},
	}
}

func (r *WorkloadOwnerResolver) ResolveIndex(ctx context.Context, idx PodIndex, now time.Time) (PodIndex, int) {
	errors := 0
	type resolution struct {
		kind string
		name string
		err  error
	}
	resolved := map[string]resolution{}
	keys := make([]string, 0, len(idx.ByContainerID))
	for key := range idx.ByContainerID {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		ref := idx.ByContainerID[key]
		ownerKey := strings.Join([]string{ref.Namespace, ref.Context.OwnerKind, ref.Context.OwnerName}, "\x00")
		result, ok := resolved[ownerKey]
		if !ok {
			result.kind, result.name, result.err = r.Resolve(ctx, ref.Namespace, ref.Context.OwnerKind, ref.Context.OwnerName, now)
			resolved[ownerKey] = result
			if result.err != nil {
				errors++
			}
		}
		if result.err == nil {
			ref.Context.WorkloadKind = result.kind
			ref.Context.WorkloadName = result.name
		} else {
			ref.Context.WorkloadKind = ""
			ref.Context.WorkloadName = ""
		}
		idx.ByContainerID[key] = ref
	}
	idx.ByPodUID = map[string][]PodRef{}
	for _, key := range keys {
		ref := idx.ByContainerID[key]
		idx.ByPodUID[ref.PodUID] = append(idx.ByPodUID[ref.PodUID], ref)
	}
	idx.WorkloadContextAvailable = true
	idx.WorkloadContextErrors = errors
	return idx, errors
}

func (r *WorkloadOwnerResolver) Resolve(ctx context.Context, namespace, kind, name string, now time.Time) (string, string, error) {
	return r.resolve(ctx, namespace, kind, name, "", now)
}

// ResolveReference binds a lookup to the owner UID recorded by the current Pod.
// Reusing a controller name after replacement must not change that Pod's owner.
func (r *WorkloadOwnerResolver) ResolveReference(ctx context.Context, namespace string, owner metav1.OwnerReference, now time.Time) (string, string, error) {
	expectedGroup := ""
	switch owner.Kind {
	case "ReplicaSet":
		expectedGroup = "apps"
	case "Job":
		expectedGroup = "batch"
	}
	if expectedGroup != "" {
		version, err := schema.ParseGroupVersion(owner.APIVersion)
		if err != nil || version.Group != expectedGroup {
			return "", "", ErrOwnerAPI
		}
	}
	return r.resolve(ctx, namespace, owner.Kind, owner.Name, string(owner.UID), now)
}

func (r *WorkloadOwnerResolver) resolve(ctx context.Context, namespace, kind, name, uid string, now time.Time) (string, string, error) {
	if kind == "" || name == "" {
		return "", "", nil
	}
	if kind != "ReplicaSet" && kind != "Job" {
		return kind, name, nil
	}
	key := strings.Join([]string{namespace, kind, name, uid}, "\x00")
	if entry, ok := r.cached(key, now); ok {
		return entry.kind, entry.name, nil
	}

	var item metav1.Object
	var err error
	switch kind {
	case "ReplicaSet":
		item, err = r.client.AppsV1().ReplicaSets(namespace).Get(ctx, name, metav1.GetOptions{})
	case "Job":
		item, err = r.client.BatchV1().Jobs(namespace).Get(ctx, name, metav1.GetOptions{})
	}
	if err != nil {
		return "", "", fmt.Errorf("resolve %s %s/%s: %w", kind, namespace, name, err)
	}
	if uid != "" && string(item.GetUID()) != uid {
		return "", "", ErrOwnerReplaced
	}
	resolvedKind, resolvedName := kind, name
	if owner := preferredOwner(item.GetOwnerReferences()); owner != nil {
		resolvedKind, resolvedName = owner.Kind, owner.Name
	}
	r.store(key, ownerCacheEntry{kind: resolvedKind, name: resolvedName, loadedAt: now, expiresAt: now.Add(r.ttl)}, now)
	return resolvedKind, resolvedName, nil
}

func (r *WorkloadOwnerResolver) cached(key string, now time.Time) (ownerCacheEntry, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry, ok := r.entries[key]
	if ok && now.Before(entry.expiresAt) {
		return entry, true
	}
	delete(r.entries, key)
	return ownerCacheEntry{}, false
}

func (r *WorkloadOwnerResolver) store(key string, entry ownerCacheEntry, now time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for existingKey, existing := range r.entries {
		if !now.Before(existing.expiresAt) {
			delete(r.entries, existingKey)
		}
	}
	if len(r.entries) >= r.maxEntries {
		oldestKey := ""
		var oldest time.Time
		for existingKey, existing := range r.entries {
			if oldestKey == "" || existing.loadedAt.Before(oldest) {
				oldestKey, oldest = existingKey, existing.loadedAt
			}
		}
		delete(r.entries, oldestKey)
	}
	r.entries[key] = entry
}
