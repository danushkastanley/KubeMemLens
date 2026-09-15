package workerprocess

import (
	"context"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
)

func TestNormalExpiryPreservesVerifiedFinalCounts(t *testing.T) {
	cmd := child(t, "expiry-result")
	request := requestFixture(t, 200*time.Millisecond)
	output := &outputFixture{}
	result, err := Run(context.Background(), cmd, request, output)
	if err != nil || result.Termination != trace.Expired || result.Counts.Produced == nil || *result.Counts.Produced != 1 || output.events != 1 {
		t.Fatal("normal expiry discarded final counters")
	}
	if !result.EndedAt.Equal(request.Deadline) {
		t.Fatal("observation end changed during teardown")
	}
	assertReaped(t, cmd)
}

func TestNormalExpiryDrainsResultAfterBoundedSlowDetach(t *testing.T) {
	cmd := child(t, "expiry-slow-result")
	request := requestFixture(t, 200*time.Millisecond)
	output := &outputFixture{}
	result, err := Run(context.Background(), cmd, request, output)
	if err != nil || result.Termination != trace.Expired || result.Counts.Produced == nil || *result.Counts.Produced != 1 || output.events != 1 {
		t.Fatal("bounded kernel-close delay discarded final counters")
	}
	if !result.EndedAt.Equal(request.Deadline) {
		t.Fatal("terminal drain extended observation")
	}
	assertReaped(t, cmd)
}

func TestExpiryDoesNotAcceptMalformedTerminalResult(t *testing.T) {
	cmd := child(t, "expiry-bad-result")
	result, err := Run(context.Background(), cmd, requestFixture(t, 200*time.Millisecond), &outputFixture{})
	if err == nil || result.Counts.Produced != nil {
		t.Fatal("expiry converted malformed output to success")
	}
	assertReaped(t, cmd)
}

func TestEarlierParentDeadlineDoesNotExtendToRequestDeadline(t *testing.T) {
	cmd := child(t, "expiry-result")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	result, err := Run(ctx, cmd, requestFixture(t, 10*time.Second), &outputFixture{})
	if err == nil || result.Counts.Produced != nil || time.Since(start) > 2*time.Second {
		t.Fatal("parent deadline was treated as full request expiry")
	}
	assertReaped(t, cmd)
}

func TestExpiryDrainDoesNotForwardMoreEvents(t *testing.T) {
	cmd := child(t, "expiry-late-event")
	output := &outputFixture{}
	_, err := Run(context.Background(), cmd, requestFixture(t, 200*time.Millisecond), output)
	if err == nil || output.events != 1 {
		t.Fatal("expiry drain forwarded another event")
	}
	assertReaped(t, cmd)
}
