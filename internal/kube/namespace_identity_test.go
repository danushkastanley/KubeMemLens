package kube

import (
	"context"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/incidentsession"
	"k8s.io/client-go/rest"
)

const activeNamespace = `{"apiVersion":"v1","kind":"Namespace","metadata":{"name":"team-a","uid":"namespace-uid"},"status":{"phase":"Active"}}`

func namespaceServer(t *testing.T, handler http.HandlerFunc) (*NamespaceIdentityReader, *rest.Config) {
	t.Helper()
	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)
	config := &rest.Config{Host: server.URL, TLSClientConfig: rest.TLSClientConfig{CAData: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})}, BearerToken: "test-token"}
	reader, err := NewNamespaceIdentityReader(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reader.Close)
	return reader, config
}

func TestNamespaceIdentityVerifiedTLSAndFreshLifetime(t *testing.T) {
	var reads atomic.Int32
	reader, config := namespaceServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/namespaces/team-a" || r.Method != "GET" || r.Header.Get("Authorization") != "Bearer test-token" || r.Header.Get("Accept-Encoding") != "" {
			t.Error("unexpected identity transport request")
		}
		body := activeNamespace
		if reads.Add(1) > 1 {
			body = strings.Replace(body, "namespace-uid", "recreated-uid", 1)
		}
		fmt.Fprint(w, body)
	})
	for _, want := range []string{"namespace-uid", "recreated-uid"} {
		got, err := reader.Lookup(context.Background(), "team-a")
		if err != nil || got != want {
			t.Fatalf("identity lookup: %q %v", got, err)
		}
	}
	if config.Insecure || config.DisableCompression {
		t.Fatal("caller TLS config mutated")
	}
}

func TestNamespaceIdentityRejectsInvalidResponses(t *testing.T) {
	for _, test := range []struct {
		name, body string
		want       error
	}{
		{"wrong name", strings.Replace(activeNamespace, "team-a", "team-b", 1), incidentsession.ErrUnavailable},
		{"wrong kind", strings.Replace(activeNamespace, "Namespace", "Pod", 1), incidentsession.ErrUnavailable},
		{"wrong version", strings.Replace(activeNamespace, "v1", "v2", 1), incidentsession.ErrUnavailable},
		{"missing uid", strings.Replace(activeNamespace, "namespace-uid", "", 1), incidentsession.ErrUnavailable},
		{"oversized uid", strings.Replace(activeNamespace, "namespace-uid", strings.Repeat("x", 129), 1), incidentsession.ErrUnavailable},
		{"terminating", strings.Replace(activeNamespace, "Active", "Terminating", 1), incidentsession.ErrNotFound},
		{"deleting", strings.Replace(activeNamespace, `"name":"team-a"`, `"name":"team-a","deletionTimestamp":"2026-09-30T00:00:00Z"`, 1), incidentsession.ErrNotFound},
		{"invalid json", `{`, incidentsession.ErrUnavailable},
		{"too large", strings.Repeat(" ", maxHealthResponse+1), incidentsession.ErrUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			reader, _ := namespaceServer(t, func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, test.body) })
			got, err := reader.Lookup(context.Background(), "team-a")
			if got != "" || !errors.Is(err, test.want) {
				t.Fatalf("got %q %v", got, err)
			}
		})
	}
	for _, status := range []int{301, 401, 403, 404, 500} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			reader, _ := namespaceServer(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(status); fmt.Fprint(w, activeNamespace) })
			if uid, err := reader.Lookup(context.Background(), "team-a"); uid != "" || !errors.Is(err, incidentsession.ErrUnavailable) {
				t.Fatal("non-success response accepted")
			}
		})
	}
}

func TestNamespaceIdentityTransportBoundaries(t *testing.T) {
	var followed atomic.Bool
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { followed.Store(true) }))
	defer target.Close()
	reader, config := namespaceServer(t, func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 302) })
	if _, err := reader.Lookup(context.Background(), "team-a"); err == nil || followed.Load() {
		t.Fatal("redirect followed")
	}
	untrusted := rest.CopyConfig(config)
	untrusted.CAData = nil
	untrustedReader, err := NewNamespaceIdentityReader(untrusted)
	if err != nil {
		t.Fatal(err)
	}
	defer untrustedReader.Close()
	if _, err := untrustedReader.Lookup(context.Background(), "team-a"); err == nil {
		t.Fatal("untrusted server accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := reader.Lookup(ctx, "team-a"); err == nil {
		t.Fatal("cancelled lookup succeeded")
	}
	for _, name := range []string{"", "../team-b", "team-a/other"} {
		if _, err := reader.Lookup(context.Background(), name); !errors.Is(err, incidentsession.ErrInvalid) {
			t.Fatal("invalid namespace accepted")
		}
	}
	for _, config := range []*rest.Config{nil, {Host: "http://example.test"}, {Host: "https://example.test", TLSClientConfig: rest.TLSClientConfig{Insecure: true}}, {Host: "https://user:secret@example.test"}, {Host: "https://example.test?x=y"}} {
		if _, err := NewNamespaceIdentityReader(config); err == nil {
			t.Fatal("invalid transport configuration accepted")
		}
	}
}
