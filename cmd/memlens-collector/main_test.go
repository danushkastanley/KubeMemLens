package main

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
)

func TestNormaliseServerErrorAcceptsExpectedShutdown(t *testing.T) {
	for _, err := range []error{nil, context.Canceled, http.ErrServerClosed} {
		if got := normaliseServerError(err); got != nil {
			t.Fatalf("normaliseServerError(%v) = %v", err, got)
		}
	}
	want := errors.New("listen failed")
	if got := normaliseServerError(want); !errors.Is(got, want) {
		t.Fatalf("normaliseServerError() = %v", got)
	}
}

func TestWaitForServerResultsWaitsForEveryServer(t *testing.T) {
	results := make(chan serverResult, 2)
	waited := make(chan error, 1)
	go func() {
		waited <- waitForServerResults(context.Background(), results, 2)
	}()

	results <- serverResult{name: "read"}
	select {
	case err := <-waited:
		t.Fatalf("returned before extension drained: %v", err)
	default:
	}
	results <- serverResult{name: "extension"}
	if err := <-waited; err != nil {
		t.Fatalf("waitForServerResults() = %v", err)
	}
}

func TestWaitForServerResultsReportsFailure(t *testing.T) {
	serverFailure := errors.New("bind failed")
	results := make(chan serverResult, 1)
	results <- serverResult{name: "extension", err: serverFailure}

	err := waitForServerResults(context.Background(), results, 1)
	if !errors.Is(err, serverFailure) || !strings.Contains(err.Error(), "extension server") {
		t.Fatalf("waitForServerResults() = %v", err)
	}
}

func TestWaitForServerResultsReportsTimeout(t *testing.T) {
	results := make(chan serverResult)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := waitForServerResults(ctx, results, 1)
	if !errors.Is(err, context.Canceled) || !strings.Contains(err.Error(), "wait for server drain") {
		t.Fatalf("waitForServerResults() = %v", err)
	}
}

func TestShutdownBudgetContainsExtensionDelay(t *testing.T) {
	if extensionShutdownDelay <= 0 || collectorShutdownLimit <= extensionShutdownDelay {
		t.Fatalf("shutdown limit %s does not contain extension delay %s", collectorShutdownLimit, extensionShutdownDelay)
	}
}
