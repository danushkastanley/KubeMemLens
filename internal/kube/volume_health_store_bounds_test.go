package kube

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/volumecontext"
	"github.com/danushkastanley/kube-memlens/internal/volumehealth"
)

func TestHealthCacheByteLimitRejectsAtomically(t *testing.T) {
	now := time.Now().UTC()
	c := newHealthCache()
	o := cacheObservation(now)
	o.Conditions = make([]volumehealth.Condition, volumehealth.MaxConditions)
	for i := range o.Conditions {
		o.Conditions[i] = volumehealth.Condition{Status: volumehealth.Status(strings.Repeat("S", 256)), Reason: strings.Repeat("r", 256), AccessMode: strings.Repeat("a", 64), VolumeMode: strings.Repeat("b", 64), TransitionAt: now}
	}
	for i := 0; i < volumecontext.MaxHealthRecords; i++ {
		key := cacheKey()
		key.nodeUID = fmt.Sprintf("node-%d", i)
		before := c.stats()
		_, err := c.observe(key, o, now)
		if err != nil {
			after := c.stats()
			if i == 0 || after.Bytes != before.Bytes || after.Entries != before.Entries || after.Bytes > volumecontext.MaxHealthBytes {
				t.Fatal("capacity rejection changed retained state")
			}
			return
		}
	}
	t.Fatal("largest statuses did not reach byte ceiling before record limit")
}

func TestCallerBudgetAndCancellationDoNotPoisonSharedHealth(t *testing.T) {
	now := time.Now().UTC()
	c := newHealthCache()
	key := cacheKey()
	if _, err := c.observe(key, cacheObservation(now), now); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	_, err := c.backend(ctx, key, func(context.Context) (volumehealth.Observation, error) {
		t.Fatal("source started without its timeout budget")
		return volumehealth.Observation{}, nil
	}, now)
	if err == nil {
		t.Fatal("short source budget accepted")
	}
	ctx2, cancel2 := context.WithCancel(t.Context())
	_, err = c.backend(ctx2, key, func(context.Context) (volumehealth.Observation, error) {
		cancel2()
		return volumehealth.Observation{}, context.Canceled
	}, now)
	if err != context.Canceled {
		t.Fatal("caller cancellation changed error", err)
	}
	c.mu.Lock()
	entry := c.entries[key]
	c.mu.Unlock()
	result, err := cachedHealth(key, entry)
	if err != nil || result.Availability != volumehealth.Reported || result.LastGood != nil {
		t.Fatal("caller cancellation became a shared source failure", err)
	}
	partial := c.unavailable(key, now)
	if partial.Availability != volumehealth.Unavailable || partial.LastGood == nil {
		t.Fatal("caller-specific failure lost explicitly historical evidence")
	}
}

func TestInvalidBackendStatusNeverReturnsAsFreshOnNextRead(t *testing.T) {
	now := time.Now().UTC()
	c := newHealthCache()
	key := cacheKey()
	if _, err := c.observe(key, cacheObservation(now), now); err != nil {
		t.Fatal(err)
	}
	load := func(context.Context) (volumehealth.Observation, error) {
		o := cacheObservation(now)
		o.Conditions[0].Status = ""
		return o, nil
	}
	for i := 0; i < 2; i++ {
		r, err := c.backend(t.Context(), key, load, now)
		if err != nil || r.Availability != volumehealth.Unavailable || r.LastGood == nil {
			t.Fatal("malformed read revived old current health", err)
		}
	}
}
