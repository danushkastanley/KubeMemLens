package kube

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/volumecontext"
	"github.com/danushkastanley/kube-memlens/internal/volumehealth"
)

func cacheObservation(at time.Time) volumehealth.Observation {
	return volumehealth.Observation{Source: volumehealth.BackendSource, Scope: volumehealth.BackendScope, Availability: volumehealth.Reported, ObservedAt: at, Conditions: []volumehealth.Condition{{Status: "StorageDegraded", Reason: "bounded-reason", Message: "private-backend-handle"}}}
}

func cacheKey() healthCacheKey {
	return healthCacheKey{source: volumehealth.BackendSource, nodeName: "node-a", nodeUID: "node-uid", driver: "fixture.csi.test"}
}

func TestHealthCacheDoesNotRewriteUnchangedPayloadOrPromoteHistory(t *testing.T) {
	now := time.Now().UTC()
	c := newHealthCache()
	key := cacheKey()
	o := cacheObservation(now)
	if _, err := c.observe(key, o, now); err != nil {
		t.Fatal(err)
	}
	for i := 1; i < 50; i++ {
		at := now.Add(time.Duration(i) * time.Second)
		o.ObservedAt = at
		if _, err := c.observe(key, o, at); err != nil {
			t.Fatal(err)
		}
	}
	if c.stats().PayloadWrites != 1 {
		t.Fatal("unchanged condition rewrote retention")
	}
	lastRead := o.ObservedAt
	o.Availability, o.Reason, o.Conditions = volumehealth.Unavailable, volumehealth.ReadFailed, nil
	o.ObservedAt = now.Add(time.Minute)
	result, err := c.observe(key, o, o.ObservedAt)
	if err != nil {
		t.Fatal(err)
	}
	if result.Availability != volumehealth.Unavailable || result.LastGood == nil || result.LastGood.ObservedAt != lastRead || len(result.LastGood.Conditions) != 1 || result.LastGood.Conditions[0].Message != "" {
		t.Fatal("failed read promoted or erased prior evidence")
	}
	result.LastGood.Conditions[0].Reason = "modified"
	result, err = c.observe(key, o, o.ObservedAt)
	if err != nil || result.LastGood.Conditions[0].Reason == "modified" {
		t.Fatal("returned state mutated cache", err)
	}
	o.ObservedAt = lastRead.Add(volumecontext.ExpireAfter + time.Second)
	result, err = c.observe(key, o, o.ObservedAt)
	if err != nil || result.LastGood != nil {
		t.Fatal("expired health retained", err)
	}
}

func TestBackendHealthReadIntervalBackoffAndRecovery(t *testing.T) {
	now := time.Now().UTC()
	c := newHealthCache()
	key := cacheKey()
	calls := 0
	fail := false
	load := func(context.Context) (volumehealth.Observation, error) {
		calls++
		if fail {
			return volumehealth.Observation{}, &HealthReadError{Reason: volumehealth.ReadFailed}
		}
		return cacheObservation(now), nil
	}
	for i := 0; i < 10; i++ {
		if _, err := c.backend(t.Context(), key, load, now); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 1 {
		t.Fatal("repeated UI reads reached the source")
	}
	now = now.Add(healthRefreshInterval)
	fail = true
	r, err := c.backend(t.Context(), key, load, now)
	if err != nil || r.Availability != volumehealth.Unavailable || r.LastGood == nil {
		t.Fatal("source failure lost last-good state", err)
	}
	now = now.Add(healthRefreshInterval)
	if _, err := c.backend(t.Context(), key, load, now); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatal("failure backoff bypassed")
	}
	now = now.Add(healthRefreshInterval)
	fail = false
	r, err = c.backend(t.Context(), key, load, now)
	if err != nil || calls != 3 || r.Availability != volumehealth.Reported || r.LastGood != nil {
		t.Fatal("recovery did not replace failure", err)
	}
}

func TestHealthCachePartitionCapacityExpiryAndShutdown(t *testing.T) {
	now := time.Now().UTC()
	c := newHealthCache()
	o := cacheObservation(now)
	for i := 0; i < volumecontext.MaxHealthRecords; i++ {
		k := cacheKey()
		k.nodeUID = fmt.Sprintf("node-%d", i)
		if _, err := c.observe(k, o, now); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := c.observe(cacheKey(), o, now); err == nil {
		t.Fatal("record limit bypassed")
	}
	if c.stats().Bytes > volumecontext.MaxHealthBytes {
		t.Fatal("byte limit bypassed")
	}
	c.mu.Lock()
	c.pruneLocked(now.Add(volumecontext.ExpireAfter + time.Second))
	c.mu.Unlock()
	if c.stats().Entries != 0 || c.stats().Bytes != 0 {
		t.Fatal("expired identities retained")
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { c.run(ctx); close(done) }()
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cache cleanup ignored cancellation")
	}
	if _, err := c.observe(cacheKey(), o, now); err == nil {
		t.Fatal("closed cache admitted a late write")
	}
}

func TestBackendHealthConcurrentReadUsesOneRequest(t *testing.T) {
	now := time.Now().UTC()
	c := newHealthCache()
	key := cacheKey()
	if _, err := c.observe(key, cacheObservation(now), now); err != nil {
		t.Fatal(err)
	}
	started, release := make(chan struct{}), make(chan struct{})
	var wg sync.WaitGroup
	wg.Go(func() {
		_, err := c.backend(t.Context(), key, func(context.Context) (volumehealth.Observation, error) {
			close(started)
			<-release
			return cacheObservation(now), nil
		}, now)
		if err != nil {
			t.Error(err)
		}
	})
	<-started
	for i := 0; i < 8; i++ {
		wg.Go(func() {
			_, err := c.backend(t.Context(), key, func(context.Context) (volumehealth.Observation, error) {
				t.Error("concurrent source request")
				return cacheObservation(now), nil
			}, now)
			if err != nil {
				t.Error(err)
			}
		})
	}
	close(release)
	wg.Wait()
	if c.stats().Reads != 1 {
		t.Fatal("source requests overlapped")
	}
}
