package incidentsession

import (
	"context"
	"errors"
	"testing"
)

func TestInspectOwnershipExpiryAndCopies(t *testing.T) {
	s, clk, auth, p := fixture(t, DefaultLimits())
	defer s.Shutdown()
	started := start(t, s, p)
	ctx := context.Background()
	other := p
	other.Actor = "another-operator"
	if _, err := s.Inspect(ctx, other, started.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("metadata leaked to other actor")
	}
	closed, err := s.Close(ctx, p, started.ID)
	if err != nil {
		t.Fatal(err)
	}
	inspected, err := s.Inspect(ctx, p, started.ID)
	if err != nil || inspected.ClosedAt == nil || !inspected.ClosedAt.Equal(*closed.ClosedAt) {
		t.Fatal("closed metadata lost")
	}
	*inspected.ClosedAt = inspected.ClosedAt.Add(DefaultLimits().Retention)
	again, err := s.Inspect(ctx, p, started.ID)
	if err != nil || !again.ClosedAt.Equal(*closed.ClosedAt) {
		t.Fatal("metadata aliases stored state")
	}
	auth.deny = true
	if _, err := s.Inspect(ctx, p, started.ID); !errors.Is(err, ErrDenied) {
		t.Fatal("metadata bypassed revocation")
	}
	auth.deny = false
	clk.Step(DefaultLimits().Retention)
	if _, err := s.Inspect(ctx, p, started.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("expired metadata returned")
	}
}
