package incidentsession

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	clocktesting "k8s.io/utils/clock/testing"
)

type policy struct {
	mu    sync.Mutex
	deny  bool
	calls []Operation
}

func (p *policy) Authorize(_ context.Context, _ Principal, op Operation, _ string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls = append(p.calls, op)
	if p.deny {
		return ErrDenied
	}
	return nil
}

func fixture(t *testing.T, limits Limits) (*Store, *clocktesting.FakeClock, *policy, Principal) {
	t.Helper()
	clk := clocktesting.NewFakeClock(time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC))
	auth := &policy{}
	s, err := newStore(context.Background(), limits, auth, clk)
	if err != nil {
		t.Fatal(err)
	}
	return s, clk, auth, Principal{"tenant", "namespace-uid", "private-operator"}
}

func start(t *testing.T, s *Store, p Principal) Summary {
	t.Helper()
	value, err := s.Start(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestExplicitLifecycleAndReservedClose(t *testing.T) {
	limits := DefaultLimits()
	limits.Entries = 3
	s, clk, auth, p := fixture(t, limits)
	defer s.Shutdown()
	ctx := context.Background()
	id := strings.Repeat("a", 32)
	if _, err := s.Append(ctx, p, id, Input{Kind: Annotated, Source: "operator", Note: "note"}); !errors.Is(err, ErrNotFound) {
		t.Fatal("implicit creation allowed")
	}
	created := start(t, s, p)
	clk.Step(time.Minute)
	value, err := s.Append(ctx, p, created.ID, Input{Kind: Annotated, Source: "operator", Note: "Compare after restart"})
	if err != nil || value.Entries != 2 || !value.ExpiresAt.Equal(created.ExpiresAt) {
		t.Fatal("append changed lifetime", err)
	}
	if _, err := s.Append(ctx, p, created.ID, Input{Kind: Annotated, Source: "operator", Note: "excess"}); !errors.Is(err, ErrCapacity) {
		t.Fatal("Entry bound not enforced")
	}
	closed, err := s.Close(ctx, p, created.ID)
	if err != nil || closed.Entries != 3 || closed.ClosedAt == nil || !closed.LimitReached {
		t.Fatal("close reserve lost", err)
	}
	*closed.ClosedAt = time.Time{}
	if _, err := s.Append(ctx, p, created.ID, Input{Kind: Annotated, Source: "operator", Note: "late"}); !errors.Is(err, ErrClosed) {
		t.Fatal("closed record changed")
	}
	data, err := s.Export(ctx, p, created.ID, ExportAuthorised)
	if err != nil {
		t.Fatal(err)
	}
	var doc AuthorisedExport
	if json.Unmarshal(data, &doc) != nil || doc.ClosedAt == nil || doc.ClosedAt.IsZero() || doc.Entries[2].Kind != Closed {
		t.Fatal("returned pointer changed private record")
	}
	if err := s.Delete(ctx, p, created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Export(ctx, p, created.ID, ExportSanitised); !errors.Is(err, ErrNotFound) {
		t.Fatal("deleted data returned")
	}
	auth.mu.Lock()
	defer auth.mu.Unlock()
	if len(auth.calls) != 9 {
		t.Fatalf("operations bypassed authorisation: %d", len(auth.calls))
	}
}

func TestTenantOwnerAndNamespaceLifetimeIsolation(t *testing.T) {
	s, _, auth, p := fixture(t, DefaultLimits())
	defer s.Shutdown()
	id := start(t, s, p).ID
	ctx := context.Background()
	for _, foreign := range []Principal{{"neighbour", p.NamespaceUID, p.Actor}, {p.Namespace, "replacement-uid", p.Actor}, {p.Namespace, p.NamespaceUID, "another-operator"}} {
		if _, err := s.Export(ctx, foreign, id, ExportAuthorised); !errors.Is(err, ErrNotFound) {
			t.Fatal("foreign read succeeded")
		}
		if _, err := s.Append(ctx, foreign, id, Input{Kind: Annotated, Source: "operator", Note: "foreign"}); !errors.Is(err, ErrNotFound) {
			t.Fatal("foreign write succeeded")
		}
		if err := s.Delete(ctx, foreign, id); !errors.Is(err, ErrNotFound) {
			t.Fatal("foreign deletion succeeded")
		}
	}
	auth.mu.Lock()
	auth.deny = true
	auth.mu.Unlock()
	if _, err := s.Export(ctx, p, id, ExportSanitised); !errors.Is(err, ErrDenied) {
		t.Fatal("revoked authority returned stored evidence")
	}
	if _, err := s.Close(ctx, p, id); !errors.Is(err, ErrDenied) {
		t.Fatal("revoked authority changed state")
	}
}

func TestGlobalAndNamespaceCapacity(t *testing.T) {
	limits := DefaultLimits()
	limits.Sessions = 2
	limits.NamespaceSessions = 1
	s, _, _, p := fixture(t, limits)
	defer s.Shutdown()
	first := start(t, s, p)
	otherOwner := p
	otherOwner.Actor = "other-owner"
	if _, err := s.Start(context.Background(), otherOwner); !errors.Is(err, ErrCapacity) {
		t.Fatal("namespace quota bypassed by another actor")
	}
	otherTenant := Principal{"other", "other-uid", p.Actor}
	start(t, s, otherTenant)
	if _, err := s.Start(context.Background(), Principal{"third", "third-uid", p.Actor}); !errors.Is(err, ErrCapacity) {
		t.Fatal("global quota bypassed")
	}
	if err := s.Delete(context.Background(), p, first.ID); err != nil {
		t.Fatal(err)
	}
	start(t, s, p)
}

func TestExpiryReclaimsIdleRecordsAndShutdownClearsState(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		limits := DefaultLimits()
		limits.Retention = time.Minute
		s, clk, _, p := fixture(t, limits)
		defer s.Shutdown()
		id := start(t, s, p).ID
		clk.Step(time.Minute)
		synctest.Wait()
		s.mu.Lock()
		remaining := len(s.records)
		s.mu.Unlock()
		if remaining != 0 {
			t.Fatal("idle expiry retained private state")
		}
		if _, err := s.Export(context.Background(), p, id, ExportAuthorised); !errors.Is(err, ErrNotFound) {
			t.Fatal("expired evidence returned")
		}
		start(t, s, p)
		s.Shutdown()
		s.mu.Lock()
		remaining = len(s.records)
		s.mu.Unlock()
		if remaining != 0 {
			t.Fatal("shutdown retained private state")
		}
		if _, err := s.Start(context.Background(), p); !errors.Is(err, ErrStopped) {
			t.Fatal("stopped store accepted a session")
		}
	})
}

func TestCancelledOperationsAndIdentifierFailureDoNotCreateState(t *testing.T) {
	s, _, _, p := fixture(t, DefaultLimits())
	defer s.Shutdown()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Start(ctx, p); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled creation accepted")
	}
	s.newID = func() (string, error) { return "", errors.New("private entropy error") }
	if _, err := s.Start(context.Background(), p); !errors.Is(err, ErrUnavailable) {
		t.Fatal("entropy failure leaked or was ignored")
	}
	s.newID = func() (string, error) { return strings.Repeat("a", 32), nil }
	start(t, s, p)
	if _, err := s.Start(context.Background(), p); !errors.Is(err, ErrUnavailable) {
		t.Fatal("duplicate identifier reused")
	}
}
