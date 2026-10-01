package delivery

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
)

func TestCeilingConnectionUsesAuthenticatedTLSAndActiveIdentity(t *testing.T) {
	start := time.Now().UTC()
	data, expected, deadline, _ := ceilingFixtureAt(t, trace.EventLimit, 1, start, 2*time.Second)
	frames := bytes.SplitAfter(data, []byte("\n"))
	var calls atomic.Int32
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.TLS == nil || r.TLS.Version != tls.VersionTLS13 || r.Header.Get("Authorization") != "Bearer fixture-token" || r.Method != http.MethodGet {
			t.Error("ceiling transport changed authenticated TLS contract")
			w.WriteHeader(http.StatusForbidden)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/stream") {
			_, _ = w.Write(frames[0])
			w.(http.Flusher).Flush()
			// These waits reproduce the fixture's producer timestamps.
			time.Sleep(time.Until(start.Add(time.Second)))
			_, _ = w.Write(frames[1])
			w.(http.Flusher).Flush()
			time.Sleep(time.Until(deadline))
			_, _ = w.Write(frames[2])
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"apiVersion": "tracing.kubememlens.io/v1alpha1", "kind": "TraceAdmission",
			"metadata": map[string]string{"name": expected.SessionID, "namespace": "tenant"}, "state": "active",
			"expiresAt": deadline, "engineDigest": expected.EngineDigest})
	}))
	server.StartTLS()
	defer server.Close()
	ca := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}))
	result, err := ConnectCeiling(t.Context(), Connection{Server: server.URL, Token: "fixture-token", CAPEM: ca}, expected)
	if err != nil || !result.TransportComplete || !result.CeilingReported || calls.Load() != 2 {
		t.Fatal(result, err, calls.Load())
	}
}

func TestCeilingTransportCannotUsePendingOrOtherActiveIdentity(t *testing.T) {
	for _, change := range []string{"pending", "namespace", "session", "engine", "denied"} {
		t.Run(change, func(t *testing.T) {
			data, expected, deadline, now := ceilingFixture(t, trace.EventLimit, 1)
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/stream") {
					_, _ = w.Write(data)
					return
				}
				state, namespace, session, engine := "active", "tenant", expected.SessionID, expected.EngineDigest
				switch change {
				case "pending":
					state = "admitted"
				case "namespace":
					namespace = "other"
				case "session":
					session = "other"
				case "engine":
					engine = "other"
				case "denied":
					w.WriteHeader(http.StatusForbidden)
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"apiVersion": "tracing.kubememlens.io/v1alpha1", "kind": "TraceAdmission",
					"metadata": map[string]string{"name": session, "namespace": namespace}, "state": state,
					"expiresAt": deadline, "engineDigest": engine})
			}))
			defer server.Close()
			result, err := readCeilingWithClient(t.Context(), server.Client(), server.URL, "fixture-token", expected, now)
			if err == nil || result.TransportComplete {
				t.Fatal("ceiling transport accepted another admission identity")
			}
		})
	}
}

func TestCeilingTransportRejectsUntrustedOriginsRedirectsAndCancellation(t *testing.T) {
	_, expected, _, _ := fixture(t, fixturePath)
	for _, server := range []string{"http://127.0.0.1:6443", "https://foreign.example:6443", "https://user@127.0.0.1:6443", "https://127.0.0.1:6443/path"} {
		if _, err := ConnectCeiling(t.Context(), Connection{Server: server, Token: "fixture", CAPEM: "invalid"}, expected); err == nil {
			t.Fatal("unowned ceiling endpoint accepted")
		}
	}
	var calls atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Redirect(w, r, "https://foreign.example", http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	client := server.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	if _, err := readCeilingWithClient(t.Context(), client, server.URL, "fixture", expected, nil); err == nil || calls.Load() != 1 {
		t.Fatal("ceiling redirect followed or accepted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := readCeilingWithClient(ctx, client, server.URL, "fixture", expected, nil); err == nil {
		t.Fatal("cancelled ceiling connection succeeded")
	}
}
