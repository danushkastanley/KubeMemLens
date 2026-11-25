//go:build !windows

package cli

import (
	"context"
	"os"
	"syscall"
	"testing"
	"time"
)

// Before this, Ctrl+C killed every command except trace outright, so a file
// being staged at that moment was left behind. The first interrupt must now
// reach the command as a cancelled context and leave the process running.
func TestFirstInterruptCancelsTheCommandContext(t *testing.T) {
	ctx, stop := interruptContext(context.Background())
	defer stop()
	if err := syscall.Kill(os.Getpid(), syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ctx.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("interrupt did not cancel the command context")
	}
}

// After an interrupt, a command usually returns the failure of the read that
// was cancelled. That must not be shown as if the cluster were at fault.
func TestInterruptReplacesTheCancelledReadsError(t *testing.T) {
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := interruptedOr(cancelled, context.Canceled); err != ErrInterrupted {
		t.Fatalf("after an interrupt: err = %v", err)
	}
	if err := interruptedOr(cancelled, nil); err != ErrInterrupted {
		t.Fatalf("after an interrupt with no command error: err = %v", err)
	}
	failure := context.DeadlineExceeded
	if err := interruptedOr(context.Background(), failure); err != failure {
		t.Fatalf("without an interrupt: err = %v", err)
	}
}
