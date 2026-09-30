package delivery

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestHTTPSStreamAndActiveStatusUseTheSameBoundIdentity(t *testing.T) {
	data, expected, deadline, now := fixture(t, fixturePath)
	var requests atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Header.Get("Authorization") != "Bearer fixture-token" || r.Method != http.MethodGet {
			t.Error("credential/method mismatch")
			w.WriteHeader(http.StatusForbidden)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/stream") {
			_, _ = w.Write(data)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"apiVersion": "tracing.kubememlens.io/v1alpha1", "kind": "TraceAdmission", "metadata": map[string]string{"name": expected.SessionID, "namespace": "tenant"}, "state": "active", "expiresAt": deadline, "engineDigest": expected.EngineDigest})
	}))
	defer server.Close()
	result, err := readWithClient(t.Context(), server.Client(), server.URL, "fixture-token", expected, now)
	if err != nil || !result.TransportComplete || requests.Load() != 2 {
		t.Fatal(result, err, requests.Load())
	}
}
func TestUntrustedOriginsRedirectsAndCancellationDoNotYieldEvidence(t *testing.T) {
	_, expected, _, _ := fixture(t, fixturePath)
	for _, server := range []string{"http://127.0.0.1:6443", "https://unowned.example:6443", "https://user@127.0.0.1:6443", "https://127.0.0.1:6443/path", "https://127.0.0.1:6443?token=secret"} {
		if _, err := Connect(t.Context(), Connection{Server: server, Token: "fixture", CAPEM: "invalid"}, expected); err == nil {
			t.Fatal("unowned endpoint accepted")
		}
	}
	var calls atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Redirect(w, r, "https://unowned.example", http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	client := server.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	if _, err := readWithClient(t.Context(), client, server.URL, "fixture", expected, nil); err == nil || calls.Load() != 1 {
		t.Fatal("redirect followed or accepted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := readWithClient(ctx, client, server.URL, "fixture", expected, nil); err == nil {
		t.Fatal("cancelled transport succeeded")
	}
}
func TestWrongActiveOwnerOrStateIsRejected(t *testing.T) {
	for _, mode := range []string{"pending", "other-owner", "denied"} {
		t.Run(mode, func(t *testing.T) {
			data, expected, deadline, now := fixture(t, fixturePath)
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/stream") {
					_, _ = w.Write(data)
					return
				}
				if mode == "denied" {
					w.WriteHeader(http.StatusForbidden)
					return
				}
				state, name := "active", expected.SessionID
				if mode == "pending" {
					state = "admitted"
				}
				if mode == "other-owner" {
					name = "other"
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"apiVersion": "tracing.kubememlens.io/v1alpha1", "kind": "TraceAdmission", "metadata": map[string]string{"name": name, "namespace": "tenant"}, "state": state, "expiresAt": deadline, "engineDigest": expected.EngineDigest})
			}))
			defer server.Close()
			if result, err := readWithClient(t.Context(), server.Client(), server.URL, "fixture", expected, now); err == nil || result.TransportComplete {
				t.Fatal("unmatched active status accepted")
			}
		})
	}
}
