package traceadmission

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"
)

func TestExpiryResumesAfterIdle(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		policy := DefaultPolicy()
		policy.PendingTTL = 250 * time.Millisecond
		h := newHarness(t, policy)
		for range 3 {
			// Advance past the previous lease and let the expiry loop go idle.
			time.Sleep(time.Second)
			synctest.Wait()
			if _, err := h.manager.Admit(t.Context(), actor("user-a"), requestFor(t, "tenant-a")); err != nil {
				t.Fatal(err)
			}
			h.binder.mu.Lock()
			binding := h.binder.bindings[len(h.binder.bindings)-1]
			h.binder.mu.Unlock()
			synctest.Wait()
			time.Sleep(200 * time.Millisecond)
			synctest.Wait()
			if binding.closed.Load() {
				t.Fatal("binding expired before its deadline")
			}
			time.Sleep(150 * time.Millisecond)
			synctest.Wait()
			if !binding.closed.Load() {
				t.Fatal("expiry did not resume within the 100ms cadence")
			}
		}
	})
}

func TestExpiryRetriesUnconfirmedCleanup(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t, DefaultPolicy())
		p := actor("user-a")
		r := requestFor(t, "tenant-a")
		a, err := h.manager.Admit(t.Context(), p, r)
		if err != nil {
			t.Fatal(err)
		}
		binding := h.binder.first()
		binding.closeMu.Lock()
		binding.closeErr = errors.New("temporary failure")
		binding.closeMu.Unlock()
		if err := h.manager.Cancel(t.Context(), p, "tenant-a", a.ID()); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("cleanup failure: %v", err)
		}
		synctest.Wait()
		time.Sleep(1100 * time.Millisecond)
		synctest.Wait()
		if _, err := h.manager.Admit(t.Context(), p, r); !errors.Is(err, ErrCapacity) {
			t.Fatalf("unconfirmed cleanup released quota: %v", err)
		}
		binding.closeMu.Lock()
		binding.closeErr = nil
		binding.closeMu.Unlock()
		time.Sleep(1100 * time.Millisecond)
		synctest.Wait()
		if !binding.closed.Load() {
			t.Fatal("cleanup retries stopped with a retained reservation")
		}
		if _, err := h.manager.Admit(t.Context(), p, r); err != nil {
			t.Fatalf("confirmed cleanup retained quota: %v", err)
		}
	})
}

func TestCloseIdleManager(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t, DefaultPolicy())
		synctest.Wait()
		if err := h.manager.Close(context.Background()); err != nil {
			t.Fatal(err)
		}
	})
}
