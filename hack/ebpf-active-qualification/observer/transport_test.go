package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"io"
	"log"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func metricsEnvelope() map[string]any {
	return map[string]any{"apiVersion": "memory.kubememlens.io/v1alpha1", "kind": "Metrics", "metadata": map[string]any{"name": "current", "creationTimestamp": nil}, "contentType": "application/openmetrics-text; version=1.0.0; charset=utf-8", "content": collectorFixture()}
}
func caFor(server *httptest.Server) string {
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}))
}
func TestAuthenticatedTLSCollectorUsesFixedRoute(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Path != "/apis/memory.kubememlens.io/v1alpha1/metrics/current" || r.Method != "GET" || r.Header.Get("Authorization") != "Bearer fixture-token" || r.TLS.Version < 0x304 {
			t.Error("unexpected transport")
		}
		_ = json.NewEncoder(w).Encode(metricsEnvelope())
	}))
	defer server.Close()
	client, err := collectorClient(server.URL, "fixture-token", caFor(server))
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseIdleConnections()
	result, err := readCollector(context.Background(), client, server.URL, "fixture-token")
	if err != nil || result.Results["accepted"] != 2 || requests.Load() != 1 {
		t.Fatal(result, err)
	}
}
func TestDeniedRedirectMalformedAndOversizedResponsesFail(t *testing.T) {
	for _, kind := range []string{"denied", "redirect", "wrong-owner", "wrong-kind", "duplicate", "oversized", "trailing"} {
		t.Run(kind, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body := metricsEnvelope()
				switch kind {
				case "denied":
					w.WriteHeader(403)
					return
				case "redirect":
					w.Header().Set("Location", "https://example.invalid")
					w.WriteHeader(302)
					return
				case "wrong-owner":
					body["metadata"] = map[string]any{"name": "elsewhere"}
				case "wrong-kind":
					body["kind"] = "Other"
				case "duplicate":
					_, _ = io.WriteString(w, `{"kind":"Metrics","kind":"Metrics"}`)
					return
				case "oversized":
					_, _ = io.WriteString(w, strings.Repeat(" ", (2<<20)+1))
					return
				}
				_ = json.NewEncoder(w).Encode(body)
				if kind == "trailing" {
					_, _ = io.WriteString(w, "{}")
				}
			}))
			defer server.Close()
			client, err := collectorClient(server.URL, "fixture-token", caFor(server))
			if err != nil {
				t.Fatal(err)
			}
			defer client.CloseIdleConnections()
			if _, err := readCollector(context.Background(), client, server.URL, "fixture-token"); err == nil {
				t.Fatal("invalid response accepted")
			} else {
				expected := map[string]string{"denied": "collector-status", "redirect": "collector-status", "oversized": "collector-body"}[kind]
				if expected == "" {
					expected = "collector-envelope"
				}
				if failureStage(err) != expected {
					t.Fatalf("failure stage = %s, want %s", failureStage(err), expected)
				}
			}
		})
	}
}
func TestWrongValidCABlocksTokenBeforeHTTP(t *testing.T) {
	var seen atomic.Int32
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { seen.Add(1) }))
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	server.StartTLS()
	defer server.Close()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	cert := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "other"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	der, err := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	client, err := collectorClient(server.URL, "fixture-token", string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})))
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseIdleConnections()
	if _, err := readCollector(context.Background(), client, server.URL, "fixture-token"); err == nil || seen.Load() != 0 {
		t.Fatal("untrusted server received authenticated HTTP")
	}
}
func TestUnapprovedEndpointsRejectedBeforeConnection(t *testing.T) {
	for _, url := range []string{"http://127.0.0.1:443", "https://example.com:443", "https://user@127.0.0.1:443", "https://127.0.0.1:443/path", "https://127.0.0.1:443?x=y"} {
		if _, err := collectorClient(url, "fixture-token", "invalid"); err == nil {
			t.Fatal("unapproved endpoint")
		}
	}
}
