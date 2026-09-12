package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"k8s.io/client-go/rest"
)

func TestFailedReadRetainsSafeFailureCategory(t *testing.T) {
	for _, scenario := range []struct{ status, code int }{
		{401, 10}, {403, 11}, {404, 12}, {429, 13}, {503, 14}, {408, 15},
	} {
		t.Run(http.StatusText(scenario.status), func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(scenario.status)
				_, _ = w.Write([]byte("private-response-payload"))
			}))
			defer server.Close()
			var output bytes.Buffer
			err := read(context.Background(), &rest.Config{Host: server.URL, Transport: server.Client().Transport},
				options{namespace: "fixture", operation: "containers"}, &output)
			if err == nil || failureExitCode(err) != scenario.code || output.Len() != 0 {
				t.Fatalf("failed read lost category or exposed a payload: code=%d, output bytes=%d", failureExitCode(err), output.Len())
			}
		})
	}
}

func TestInvalidResponseAndCancellationRemainDistinct(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("private-invalid-json"))
	}))
	defer server.Close()
	err := read(context.Background(), &rest.Config{Host: server.URL, Transport: server.Client().Transport},
		options{namespace: "fixture", operation: "containers"}, &bytes.Buffer{})
	if failureExitCode(err) != 18 || failureExitCode(context.Canceled) != 16 || failureExitCode(context.DeadlineExceeded) != 15 {
		t.Fatal("response, cancellation and deadline failures must remain distinct")
	}
}
