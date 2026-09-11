package client

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/capability"
)

func TestAccessDiscoveryRejectsInvalidOrUnboundedResponses(t *testing.T) {
	for _, test := range []struct{ name, body string }{
		{"invalid JSON", "{"},
		{"trailing JSON", `{"kind":"SelfSubjectAccessReview"} {}`},
		{"wrong type", `{"apiVersion":"v1","kind":"Pod","status":{"allowed":true}}`},
		{"evaluation failure", `{"apiVersion":"authorization.k8s.io/v1","kind":"SelfSubjectAccessReview","status":{"allowed":true,"evaluationError":"private-provider-message"}}`},
		{"contradictory decision", `{"apiVersion":"authorization.k8s.io/v1","kind":"SelfSubjectAccessReview","status":{"allowed":true,"denied":true}}`},
		{"oversized", strings.Repeat(" ", (1<<20)+1)},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, test.body) }))
			defer server.Close()
			state, err := discoverAccess(context.Background(), evidenceOptions(t, server), "", "pods")
			if err != nil || state.Availability != capability.Unavailable || state.Reason != capability.InvalidResponse {
				t.Fatalf("state=%+v err=%v", state, err)
			}
			if strings.Contains(fmt.Sprint(state), "private-provider-message") {
				t.Fatal("provider text escaped discovery")
			}
		})
	}
}

func TestAccessDiscoveryDenialAndUnknownReviewAreDistinct(t *testing.T) {
	for _, test := range []struct {
		code   int
		want   capability.Availability
		reason capability.Reason
	}{
		{401, capability.Unavailable, capability.AuthenticationFailed},
		{403, capability.Forbidden, capability.AccessDenied},
		{404, capability.Unavailable, capability.RequestFailed},
		{500, capability.Unavailable, capability.RequestFailed},
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(test.code) }))
		state, err := discoverAccess(context.Background(), evidenceOptions(t, server), "", "pods")
		server.Close()
		if err != nil || state.Availability != test.want || state.Reason != test.reason {
			t.Fatalf("code=%d state=%+v err=%v", test.code, state, err)
		}
	}
}

func TestAccessDiscoveryDoesNotFollowRedirects(t *testing.T) {
	received := false
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { received = true }))
	defer destination.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, destination.URL, 307) }))
	defer server.Close()
	state, err := discoverAccess(context.Background(), evidenceOptions(t, server), "", "pods")
	if err != nil || state.Availability == capability.Available || received {
		t.Fatalf("state=%+v redirected=%v err=%v", state, received, err)
	}
}

func TestEvidenceDiscoveryCancelsInFlightTransport(t *testing.T) {
	started := make(chan struct{})
	finished := make(chan struct{})
	cleanup := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Drain the POST body so net/http can observe the closed connection.
		_, _ = io.Copy(io.Discard, io.LimitReader(r.Body, 4096))
		close(started)
		select {
		case <-r.Context().Done():
		case <-cleanup:
		}
		close(finished)
	}))
	defer server.Close()
	defer close(cleanup)
	opts := evidenceOptions(t, server)
	opts.EvidenceMode = capability.Restricted
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() { _, err := NewEvidenceSession(ctx, opts); result <- err }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("request did not start")
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("discovery did not cancel")
	}
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("HTTP request continued after cancellation")
	}
}

func TestExplicitAllNamespaceAccessReviewDoesNotEnumerateNamespaces(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/apis/authorization.k8s.io/v1/selfsubjectaccessreviews" {
			t.Errorf("unexpected path %s", r.URL)
		}
		fmt.Fprint(w, `{"apiVersion":"authorization.k8s.io/v1","kind":"SelfSubjectAccessReview","status":{"allowed":false,"reason":"private-namespace"}}`)
	}))
	defer server.Close()
	opts := evidenceOptions(t, server)
	opts.ReadScope, opts.EvidenceMode = AllNamespacesScope(), capability.Restricted
	session, err := NewEvidenceSession(context.Background(), opts)
	if err == nil || session.Plan.Sources[0].Availability != capability.Forbidden {
		t.Fatalf("session=%+v err=%v", session, err)
	}
	if strings.Contains(err.Error(), "private-namespace") {
		t.Fatal("raw authorisation reason leaked")
	}
}
