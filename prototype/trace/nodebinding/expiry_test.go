package nodebinding

import (
	"context"
	"errors"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	admission "github.com/danushkastanley/kube-memlens/internal/traceadmission"
	"github.com/danushkastanley/kube-memlens/internal/tracepreflight"
	"github.com/danushkastanley/kube-memlens/prototype/trace/targetfs"
)

func expiryService(t *testing.T, preflight Preflight) (*Service, <-chan *testHandle) {
	t.Helper()
	_, peer := newCertificates(t).issue(t)
	handles := make(chan *testHandle, 4)
	s, err := NewService(t.Context(), "node-uid", "node", peer,
		func(_ context.Context, w admission.Workload) (targetfs.Handle, error) {
			target := w.Target
			target.CgroupID = 123
			h := &testHandle{target: target, closed: make(chan struct{})}
			handles <- h
			return h, nil
		}, preflight, func(string) {}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	return s, handles
}

func TestExpiryResumesAfterIdle(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s, handles := expiryService(t, func(context.Context) (string, error) {
			return tracepreflight.Baseline().Digest(), nil
		})
		for range 3 {
			time.Sleep(time.Second)
			synctest.Wait()
			r := requestFor(strings.Repeat("a", 32), workload(), testIntent(), time.Now().Add(250*time.Millisecond))
			if _, err := s.bind(t.Context(), r); err != nil {
				t.Fatal(err)
			}
			h := <-handles
			synctest.Wait()
			time.Sleep(200 * time.Millisecond)
			synctest.Wait()
			select {
			case <-h.closed:
				t.Fatal("lease expired before its deadline")
			default:
			}
			time.Sleep(150 * time.Millisecond)
			synctest.Wait()
			select {
			case <-h.closed:
			default:
				t.Fatal("expiry did not resume within the 100ms cadence")
			}
			s.mu.Lock()
			remaining := len(s.leases) + len(s.seen)
			s.mu.Unlock()
			if remaining != 0 {
				t.Fatal("expiry retained the lease or replay nonce")
			}
		}
	})
}

func TestReplayOnlyStateExpiresWithoutRequests(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s, handles := expiryService(t, func(context.Context) (string, error) {
			return "", admission.ErrUnavailable
		})
		synctest.Wait()
		r := requestFor(strings.Repeat("b", 32), workload(), testIntent(), time.Now().Add(250*time.Millisecond))
		if _, err := s.bind(t.Context(), r); !errors.Is(err, admission.ErrUnavailable) {
			t.Fatalf("preflight failure: %v", err)
		}
		synctest.Wait()
		time.Sleep(200 * time.Millisecond)
		synctest.Wait()
		if _, err := s.bind(t.Context(), r); !errors.Is(err, admission.ErrExpired) {
			t.Fatalf("live replay nonce was forgotten: %v", err)
		}
		time.Sleep(150 * time.Millisecond)
		synctest.Wait()
		s.mu.Lock()
		remaining := len(s.seen)
		s.mu.Unlock()
		if remaining != 0 || len(handles) != 0 {
			t.Fatal("failed bind retained a nonce after expiry or resolved a handle")
		}
	})
}

func TestCloseIdleService(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s, _ := expiryService(t, func(context.Context) (string, error) {
			return tracepreflight.Baseline().Digest(), nil
		})
		synctest.Wait()
		if err := s.Close(context.Background()); err != nil {
			t.Fatal(err)
		}
	})
}
