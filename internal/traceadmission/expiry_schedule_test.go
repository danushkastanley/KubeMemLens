package traceadmission

import (
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

func TestExpirySchedulePreservesDeadlinesAndCleanupRetry(t *testing.T) {
	now := time.Unix(100, 0)
	for _, tc := range []struct {
		name    string
		entries map[string]*entry
		want    time.Time
	}{
		{"empty", nil, time.Time{}},
		{"pending", map[string]*entry{"a": {expires: now.Add(time.Second)}}, now.Add(time.Second)},
		{"earliest active", map[string]*entry{
			"a": {stage: active, expires: now.Add(time.Second)},
			"b": {expires: now.Add(2 * time.Second)},
		}, now.Add(time.Second)},
		{"expired", map[string]*entry{"a": {expires: now.Add(-time.Second)}}, now.Add(-time.Second)},
		{"unconfirmed retry", map[string]*entry{"a": {stage: closing, retryCleanup: now.Add(time.Second)}}, now.Add(time.Second)},
		{"unfinished cleanup", map[string]*entry{"a": {stage: closing}}, now.Add(100 * time.Millisecond)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := &Manager{entries: tc.entries}
			if got := m.nextExpiry(now); !got.Equal(tc.want) {
				t.Fatalf("next expiry=%v, want %v", got, tc.want)
			}
		})
	}
}

func TestClaimReschedulesEarlierDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		policy := DefaultPolicy()
		policy.PendingTTL = 10 * time.Second
		h := newHarness(t, policy)
		r, err := DecodeRequest("tenant-a", strings.NewReader(`{"schemaVersion":1,"pod":"target","container":"worker","kind":"files","durationSeconds":1}`))
		if err != nil {
			t.Fatal(err)
		}
		p := actor("user-a")
		a, err := h.manager.Admit(t.Context(), p, r)
		if err != nil {
			t.Fatal(err)
		}
		synctest.Wait() // The expiry loop is waiting for the longer pending TTL.
		time.Sleep(250 * time.Millisecond)
		lease, err := h.manager.Claim(t.Context(), p, "tenant-a", a.ID())
		if err != nil {
			t.Fatal(err)
		}
		defer lease.stopParent()
		// A consumer that has stopped still cannot release the binding before
		// the active deadline unless it explicitly closes its lease.
		h.manager.endConsumer(a.ID())
		synctest.Wait()
		time.Sleep(950 * time.Millisecond)
		synctest.Wait()
		if h.binder.first().closed.Load() {
			t.Fatal("claim deadline was shortened")
		}
		time.Sleep(50 * time.Millisecond)
		synctest.Wait()
		if !h.binder.first().closed.Load() {
			t.Fatal("expiry remained asleep on the previous pending TTL")
		}
	})
}
