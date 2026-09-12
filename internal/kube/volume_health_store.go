package kube

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/volumecontext"
	"github.com/danushkastanley/kube-memlens/internal/volumehealth"
	"golang.org/x/time/rate"
)

const healthRefreshInterval = 15 * time.Second

// Only acquisition data enters this cache. Caller access decisions never do.
type healthCacheKey struct {
	source                                                           volumehealth.Source
	namespace, nodeName, nodeUID, podUID, volumeName, pvcUID, driver string
}

func (k healthCacheKey) bytes() int {
	return len(k.source) + len(k.namespace) + len(k.nodeName) + len(k.nodeUID) + len(k.podUID) + len(k.volumeName) + len(k.pvcUID) + len(k.driver)
}

type healthCacheEntry struct {
	current, good                   volumecontext.HealthPayload
	observedAt, goodAt, attemptedAt time.Time
	loading                         bool
	backoff                         uint
}

func (e healthCacheEntry) bytes(k healthCacheKey) int {
	return e.current.Size() + e.good.Size() + k.bytes() + 1024
}

type healthCache struct {
	mu                                 sync.Mutex
	entries                            map[healthCacheKey]healthCacheEntry
	bytes                              int
	writes, reads, failures, throttled uint64
	requests                           *rate.Limiter
	inflight                           chan struct{}
	closed                             bool
}

type healthCacheStats struct {
	Entries                                   int
	Bytes                                     int
	PayloadWrites, Reads, Failures, Throttled uint64
}

func newHealthCache() *healthCache {
	return &healthCache{entries: map[healthCacheKey]healthCacheEntry{}, requests: rate.NewLimiter(5, 10), inflight: make(chan struct{}, 4)}
}

func (c *healthCache) run(ctx context.Context) {
	ticker := time.NewTicker(healthRefreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			c.mu.Lock()
			c.closed = true
			clear(c.entries)
			c.bytes = 0
			c.mu.Unlock()
			return
		case now := <-ticker.C:
			c.mu.Lock()
			c.pruneLocked(now)
			c.mu.Unlock()
		}
	}
}

func (c *healthCache) stats() healthCacheStats {
	c.mu.Lock()
	defer c.mu.Unlock()
	return healthCacheStats{Entries: len(c.entries), Bytes: c.bytes, PayloadWrites: c.writes, Reads: c.reads, Failures: c.failures, Throttled: c.throttled}
}

func (c *healthCache) observe(k healthCacheKey, o volumehealth.Observation, now time.Time) (volumecontext.HealthObservation, error) {
	payload, err := volumecontext.NewHealthPayload(o, now)
	if err != nil {
		return volumecontext.HealthObservation{}, err
	}
	if k.bytes() > 1024 || k.nodeUID == "" || k.source != o.Source {
		return volumecontext.HealthObservation{}, volumecontext.ErrInvalid
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pruneLocked(now)
	if c.closed {
		return volumecontext.HealthObservation{}, errors.New("health acquisition is closed")
	}
	prior, exists := c.entries[k]
	if !exists && len(c.entries) >= volumecontext.MaxHealthRecords {
		return volumecontext.HealthObservation{}, errors.New("health retention capacity exceeded")
	}
	entry := prior
	changed := !prior.current.Equal(payload)
	if changed {
		entry.current = payload
	}
	entry.observedAt = o.ObservedAt
	if o.Availability == volumehealth.Unavailable || o.Availability == volumehealth.Forbidden {
		entry.backoff = min(prior.backoff+1, 2)
	} else {
		entry.backoff = 0
	}
	if o.Availability == volumehealth.Reported {
		entry.good, entry.goodAt = entry.current, o.ObservedAt
	} else if o.Availability != volumehealth.Unavailable && o.Availability != volumehealth.Unreported {
		entry.good, entry.goodAt = volumecontext.HealthPayload{}, time.Time{}
	}
	if now.Sub(entry.goodAt) > volumecontext.ExpireAfter {
		entry.good, entry.goodAt = volumecontext.HealthPayload{}, time.Time{}
	}
	previousBytes := 0
	if exists {
		previousBytes = prior.bytes(k)
	}
	if c.bytes-previousBytes+entry.bytes(k) > volumecontext.MaxHealthBytes {
		return volumecontext.HealthObservation{}, errors.New("health retention capacity exceeded")
	}
	c.bytes += entry.bytes(k) - previousBytes
	c.entries[k] = entry
	if changed {
		c.writes++
	}
	return cachedHealth(k, entry)
}

func cachedHealth(k healthCacheKey, entry healthCacheEntry) (volumecontext.HealthObservation, error) {
	o, err := entry.current.Observation(entry.observedAt)
	if err != nil {
		return volumecontext.HealthObservation{}, err
	}
	result := volumecontext.HealthObservation{Observation: o, NodeUID: k.nodeUID}
	if o.Availability != volumehealth.Reported && entry.good.Size() > 0 {
		good, err := entry.good.Observation(entry.goodAt)
		if err != nil {
			return volumecontext.HealthObservation{}, err
		}
		result.LastGood = &good
	}
	return result, nil
}

func (c *healthCache) pruneLocked(now time.Time) {
	for key, entry := range c.entries {
		if entry.loading {
			continue
		}
		if now.Sub(entry.observedAt) > volumecontext.ExpireAfter {
			c.bytes -= entry.bytes(key)
			delete(c.entries, key)
			continue
		}
		if entry.good.Size() > 0 && now.Sub(entry.goodAt) > volumecontext.ExpireAfter {
			c.bytes -= entry.good.Size()
			entry.good = volumecontext.HealthPayload{}
			entry.goodAt = time.Time{}
			c.entries[key] = entry
		}
	}
}

// backend performs at most one target GET per key per interval, with bounded
// global request rate/concurrency. Load runs without holding the cache lock.
func (c *healthCache) backend(ctx context.Context, k healthCacheKey, load func(context.Context) (volumehealth.Observation, error), now time.Time) (volumecontext.HealthObservation, error) {
	if ctx.Err() != nil {
		return volumecontext.HealthObservation{}, ctx.Err()
	}
	if k.bytes() > 1024 || k.nodeUID == "" || k.source != volumehealth.BackendSource {
		return volumecontext.HealthObservation{}, volumecontext.ErrInvalid
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return volumecontext.HealthObservation{}, errors.New("health acquisition is closed")
	}
	c.pruneLocked(now)
	entry, exists := c.entries[k]
	if exists && (entry.loading || now.Sub(entry.attemptedAt) < healthRefreshInterval<<entry.backoff) {
		c.mu.Unlock()
		return cachedHealth(k, entry)
	}
	if !exists && len(c.entries) >= volumecontext.MaxHealthRecords {
		c.mu.Unlock()
		return volumecontext.HealthObservation{}, errors.New("health retention capacity exceeded")
	}
	// Do not turn a caller's exhausted binding budget into a shared backend
	// failure. Reserve the full source timeout before starting a new request.
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < 2100*time.Millisecond {
		c.mu.Unlock()
		return volumecontext.HealthObservation{}, errors.New("health read budget is exhausted")
	}
	select {
	case c.inflight <- struct{}{}:
	default:
		c.throttled++
		c.mu.Unlock()
		return volumecontext.HealthObservation{}, errors.New("health acquisition is busy")
	}
	if !c.requests.AllowN(now, 1) {
		<-c.inflight
		c.throttled++
		c.mu.Unlock()
		return volumecontext.HealthObservation{}, errors.New("health acquisition is rate limited")
	}
	// A cold pending entry has no status; it cannot masquerade as a healthy read.
	if !exists {
		if k.bytes() > 1024 || c.bytes+entry.bytes(k) > volumecontext.MaxHealthBytes {
			<-c.inflight
			c.mu.Unlock()
			return volumecontext.HealthObservation{}, errors.New("health retention capacity exceeded")
		}
		c.bytes += entry.bytes(k)
	}
	entry.loading, entry.attemptedAt = true, now
	c.entries[k] = entry
	c.reads++
	c.mu.Unlock()
	defer func() {
		<-c.inflight
		c.mu.Lock()
		e, ok := c.entries[k]
		if ok {
			e.loading = false
			c.entries[k] = e
		}
		c.mu.Unlock()
	}()
	readCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	o, err := load(readCtx)
	if errors.Is(ctx.Err(), context.Canceled) {
		return volumecontext.HealthObservation{}, ctx.Err()
	}
	if err != nil {
		c.mu.Lock()
		c.failures++
		c.mu.Unlock()
		o = newHealthObservation(volumehealth.Identity{}, volumehealth.BackendSource)
		o.Availability, o.Reason = healthFailure(err)
		o.ObservedAt = now
	}
	result, err := c.observe(k, o, now)
	if err == nil {
		return result, nil
	}
	failure := newHealthObservation(volumehealth.Identity{}, k.source)
	failure.Availability, failure.Reason, failure.ObservedAt = volumehealth.Unavailable, volumehealth.InvalidResponse, now
	return c.observe(k, failure, now)
}

// unavailable does not write caller-specific budget/cancellation failures into
// the shared source cache. Only already authorised callers may request it.
func (c *healthCache) unavailable(k healthCacheKey, now time.Time) volumecontext.HealthObservation {
	o := newHealthObservation(volumehealth.Identity{}, k.source)
	o.Availability, o.Reason, o.ObservedAt = volumehealth.Unavailable, volumehealth.ReadFailed, now
	result := volumecontext.HealthObservation{Observation: o, NodeUID: k.nodeUID}
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, exists := c.entries[k]
	if !c.closed && exists && entry.good.Size() > 0 && now.Sub(entry.goodAt) <= volumecontext.ExpireAfter {
		good, err := entry.good.Observation(entry.goodAt)
		if err == nil {
			result.LastGood = &good
		}
	}
	return result
}
