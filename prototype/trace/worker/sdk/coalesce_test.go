package sdk

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestFileCoalescingNeverPostponesTargetValidation(t *testing.T) {
	now := time.Unix(100, 0)
	for _, delta := range []time.Duration{-time.Millisecond, 0, time.Microsecond, time.Millisecond, time.Second} {
		got := coalesceDelay(now, now.Add(delta))
		if got < 0 || got > time.Millisecond || got > max(0, delta) {
			t.Fatal("coalescing exceeded its window or postponed validation")
		}
		if delta >= time.Millisecond && got != time.Millisecond {
			t.Fatal("burst did not retain the fixed coalescing interval")
		}
	}
}

func TestFileCoalescingPreservesCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, validation := range []time.Time{time.Now().Add(time.Second), time.Now().Add(-time.Second)} {
		if err := coalesceFileStart(ctx, validation); !errors.Is(err, context.Canceled) {
			t.Fatal("coalescing hid cancellation")
		}
	}
}

func TestFileCoalescingDoesNotRequireAnotherEvent(t *testing.T) {
	ctx, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	if err := coalesceFileStart(ctx, time.Now().Add(time.Second)); err != nil {
		t.Fatal("sparse event could not complete its coalescing interval")
	}
	if err := coalesceFileStart(ctx, time.Now().Add(-time.Second)); err != nil {
		t.Fatal("due validation gained a delay")
	}
}
