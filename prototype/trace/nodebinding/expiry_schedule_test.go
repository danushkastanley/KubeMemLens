package nodebinding

import (
	"context"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/tracepreflight"
)

func TestExpiryScheduleRetainsActiveReferencesAndReplayRecords(t *testing.T) {
	now := time.Unix(100, 0)
	for _, tc := range []struct {
		name   string
		leases map[string]*lease
		seen   map[string]time.Time
		want   time.Time
	}{
		{"empty", nil, nil, time.Time{}},
		{"pending", map[string]*lease{"a": {expires: now.Add(time.Second)}}, nil, now.Add(time.Second)},
		{"earlier replay", map[string]*lease{"a": {expires: now.Add(2 * time.Second)}}, map[string]time.Time{"b": now.Add(time.Second)}, now.Add(time.Second)},
		{"expired pending", map[string]*lease{"a": {expires: now.Add(-time.Second)}}, nil, now.Add(-time.Second)},
		{"expired execution", map[string]*lease{"a": {expires: now.Add(-time.Second), execution: &execution{}}}, map[string]time.Time{"a": now.Add(-time.Second)}, time.Time{}},
		{"expired replay", nil, map[string]time.Time{"a": now.Add(-time.Second)}, now.Add(-time.Second)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &Service{leases: tc.leases, seen: tc.seen}
			if got := s.nextExpiry(); !got.Equal(tc.want) {
				t.Fatalf("next expiry=%v, want %v", got, tc.want)
			}
		})
	}
}

func TestNewBindingReschedulesEarlierExpiry(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s, handles := expiryService(t, func(context.Context) (string, error) {
			return tracepreflight.Baseline().Digest(), nil
		})
		bind := func(id string, duration time.Duration) *testHandle {
			t.Helper()
			r := requestFor(strings.Repeat(id, 32), workload(), testIntent(), time.Now().Add(duration))
			if _, err := s.bind(t.Context(), r); err != nil {
				t.Fatal(err)
			}
			return <-handles
		}
		first := bind("a", 5*time.Second)
		synctest.Wait()
		time.Sleep(20 * time.Millisecond)
		second := bind("b", 250*time.Millisecond)
		synctest.Wait()
		time.Sleep(249 * time.Millisecond)
		synctest.Wait()
		select {
		case <-second.closed:
			t.Fatal("new binding expired early")
		default:
		}
		time.Sleep(time.Millisecond)
		synctest.Wait()
		select {
		case <-second.closed:
		default:
			t.Fatal("expiry remained asleep on the previous binding")
		}
		select {
		case <-first.closed:
			t.Fatal("earlier expiry shortened another binding")
		default:
		}
	})
}

func TestFinishingExecutionReschedulesReplayExpiry(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s, handles := expiryService(t, func(context.Context) (string, error) {
			return tracepreflight.Baseline().Digest(), nil
		})
		id := strings.Repeat("c", 32)
		r := requestFor(id, workload(), testIntent(), time.Now().Add(5*time.Second))
		if _, err := s.bind(t.Context(), r); err != nil {
			t.Fatal(err)
		}
		<-handles
		synctest.Wait()
		e, err := s.activate(t.Context(), id, time.Now().Add(250*time.Millisecond))
		if err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		time.Sleep(100 * time.Millisecond)
		if err := e.finish(); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		time.Sleep(149 * time.Millisecond)
		synctest.Wait()
		s.mu.Lock()
		retained := len(s.seen) == 1
		s.mu.Unlock()
		if !retained {
			t.Fatal("finished execution lost an unexpired replay record")
		}
		time.Sleep(time.Millisecond)
		synctest.Wait()
		s.mu.Lock()
		remaining := len(s.leases) + len(s.seen)
		s.mu.Unlock()
		if remaining != 0 {
			t.Fatal("expired replay record retained after confirmed teardown")
		}
	})
}

func TestSweepDoesNotPreemptActiveDeadlineContext(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s, handles := expiryService(t, func(context.Context) (string, error) {
			return tracepreflight.Baseline().Digest(), nil
		})
		id := strings.Repeat("d", 32)
		r := requestFor(id, workload(), testIntent(), time.Now().Add(5*time.Second))
		if _, err := s.bind(t.Context(), r); err != nil {
			t.Fatal(err)
		}
		handle := <-handles
		deadline := time.Now().Add(time.Second)
		e, err := s.activate(t.Context(), id, deadline)
		if err != nil {
			t.Fatal(err)
		}
		// Exercise a sweep observing the boundary before the context timer's
		// callback. The execution timer is the authority for natural expiry.
		s.mu.Lock()
		s.prune(deadline)
		s.mu.Unlock()
		if e.ctx.Err() != nil {
			t.Fatal("sweep converted natural expiry into explicit cancellation")
		}
		time.Sleep(time.Second)
		synctest.Wait()
		if e.ctx.Err() != context.DeadlineExceeded {
			t.Fatal("active deadline did not expire independently")
		}
		select {
		case <-handle.closed:
			t.Fatal("deadline released an unconfirmed execution")
		default:
		}
		if err := e.finish(); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		s.mu.Lock()
		remaining := len(s.leases) + len(s.seen)
		s.mu.Unlock()
		if remaining != 0 {
			t.Fatal("finished execution retained expired state")
		}
	})
}
