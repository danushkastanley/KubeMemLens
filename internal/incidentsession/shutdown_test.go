package incidentsession

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
)

type authorizeFunc func(context.Context, Principal, Operation, string) error

func (f authorizeFunc) Authorize(ctx context.Context, p Principal, op Operation, id string) error {
	return f(ctx, p, op, id)
}

func TestShutdownCancelsAuthorisationAndPreventsLateCreation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		entered := make(chan struct{})
		auth := authorizeFunc(func(ctx context.Context, _ Principal, _ Operation, _ string) error {
			close(entered)
			<-ctx.Done()
			return ctx.Err()
		})
		s, err := New(context.Background(), DefaultLimits(), auth)
		if err != nil {
			t.Fatal(err)
		}
		defer s.Shutdown()
		result := make(chan error, 1)
		go func() { _, err := s.Start(context.Background(), Principal{"tenant", "uid", "actor"}); result <- err }()
		<-entered
		s.Shutdown()
		synctest.Wait()
		if err := <-result; !errors.Is(err, ErrStopped) {
			t.Fatal("shutdown did not stop the in-flight operation")
		}
		s.mu.Lock()
		count := len(s.records)
		s.mu.Unlock()
		if count != 0 {
			t.Fatal("late authorisation recreated cleared state")
		}
	})
}

func TestMissingAuthorityAndInvalidLimitsFailClosed(t *testing.T) {
	if s, err := New(context.Background(), DefaultLimits(), nil); err == nil || s != nil {
		t.Fatal("missing authority accepted")
	}
	limits := DefaultLimits()
	limits.Retention = 0
	if s, err := New(context.Background(), limits, &policy{}); err == nil || s != nil {
		t.Fatal("unbounded retention accepted")
	}
	limits = DefaultLimits()
	limits.Sessions = 65
	if s, err := New(context.Background(), limits, &policy{}); err == nil || s != nil {
		t.Fatal("global hard ceiling exceeded")
	}
}
