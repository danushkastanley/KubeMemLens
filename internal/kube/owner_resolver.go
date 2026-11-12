package kube

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"sort"
	"strings"
	"sync"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
)

const (
	defaultOwnerCacheEntries = 2000

	// ownerFailureHold is how long a failed lookup is remembered. Scans run
	// every few seconds; without a hold, an API that refuses or fails owner
	// reads would be asked again for every owner on every scan.
	ownerFailureHold = 30 * time.Second
)

var ErrOwnerReplaced = errors.New("workload owner identity changed")
var ErrOwnerAPI = errors.New("workload owner API is unsupported")

// ownerCacheEntry is either a resolved owner or, when err is set, a lookup
// that failed. loadedAt is when the owner was last read successfully, or when
// the failure was recorded.
type ownerCacheEntry struct {
	kind      string
	name      string
	err       error
	loadedAt  time.Time
	expiresAt time.Time
}

// usableWhenRefreshFails reports whether a resolved owner may still be served
// after its lifetime because the API could not be asked again. A controller's
// own owner does not change, so the last answer is better evidence than none,
// but only for a bounded time.
func (e ownerCacheEntry) usableWhenRefreshFails(now time.Time, ttl time.Duration) bool {
	return e.err == nil && now.Before(e.loadedAt.Add(2*ttl))
}

// WorkloadOwnerResolver follows only the Kubernetes controller hops needed to
// turn ReplicaSet and Job owners into top-level Deployment and CronJob owners.
// Lookups are cached to avoid adding API traffic to every scan: successes for
// the cache lifetime, spread so that they do not all expire together, and
// failures for a short hold.
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
	previous, known := r.lookup(key)
	if known && now.Before(previous.expiresAt) {
		return previous.kind, previous.name, previous.err
	}

	resolvedKind, resolvedName, err := r.read(ctx, namespace, kind, name, uid)
	if err == nil {
		r.store(key, ownerCacheEntry{kind: resolvedKind, name: resolvedName, loadedAt: now, expiresAt: now.Add(r.ttl + expirySpread(key, r.ttl))}, now)
		return resolvedKind, resolvedName, nil
	}
	keepPrevious := known && previous.usableWhenRefreshFails(now, r.ttl) && !errors.Is(err, ErrOwnerReplaced)
	if answeredByAPI(err) {
		held := ownerCacheEntry{err: err, loadedAt: now, expiresAt: now.Add(ownerFailureHold)}
		if keepPrevious {
			held = previous
			held.expiresAt = now.Add(ownerFailureHold)
		}
		r.store(key, held, now)
	}
	if keepPrevious {
		return previous.kind, previous.name, nil
	}
	return "", "", err
}

// answeredByAPI reports whether a failed lookup is an answer about the owner:
// the API server refused or could not serve the read, or the controller was
// replaced. Only an answer is held. A lookup that never reached the server,
// because the scan's time budget ran out, the client rate limiter would have
// overrun it, or the connection failed, says nothing about the owner and is
// tried again by the next scan.
func answeredByAPI(err error) bool {
	var status apierrors.APIStatus
	return errors.Is(err, ErrOwnerReplaced) || errors.As(err, &status)
}

// read asks the API for the controller and returns its top-level owner.
func (r *WorkloadOwnerResolver) read(ctx context.Context, namespace, kind, name, uid string) (string, string, error) {
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
	if owner := preferredOwner(item.GetOwnerReferences()); owner != nil {
		return owner.Kind, owner.Name, nil
	}
	return kind, name, nil
}

// expirySpread adds up to a fifth of the lifetime, fixed per owner, so that
// owners first seen in one scan do not all need a refresh in the same scan.
func expirySpread(key string, ttl time.Duration) time.Duration {
	spread := ttl / 5
	if spread <= 0 {
		return 0
	}
	hash := fnv.New64a()
	_, _ = hash.Write([]byte(key))
	return time.Duration(hash.Sum64() % uint64(spread))
}

func (r *WorkloadOwnerResolver) lookup(key string) (ownerCacheEntry, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry, ok := r.entries[key]
	return entry, ok
}

func (r *WorkloadOwnerResolver) store(key string, entry ownerCacheEntry, now time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for existingKey, existing := range r.entries {
		if !now.Before(existing.expiresAt) && !existing.usableWhenRefreshFails(now, r.ttl) {
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
