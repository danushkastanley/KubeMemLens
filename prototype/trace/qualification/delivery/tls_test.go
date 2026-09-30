package delivery

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
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

func TestProductionTLSConnectionValidatesPrivateCAAndConcurrentStatus(t *testing.T) {
	start := time.Now().UTC()
	data, expected, deadline, _ := fixtureAt(t, fixturePath, start, 2*time.Second)
	frames := bytes.SplitAfter(data, []byte("\n"))
	var calls atomic.Int32
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.TLS == nil || r.TLS.Version != tls.VersionTLS13 || r.Header.Get("Authorization") != "Bearer fixture-token" {
			t.Error("authenticated TLS contract changed")
			w.WriteHeader(403)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/stream") {
			_, _ = w.Write(frames[0])
			w.(http.Flusher).Flush()
			// Scheduled producer timestamps, not retry sleeps or relaxed assertions.
			time.Sleep(time.Until(start.Add(time.Second)))
			_, _ = w.Write(frames[1])
			w.(http.Flusher).Flush()
			time.Sleep(time.Until(deadline))
			_, _ = w.Write(frames[2])
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"apiVersion": "tracing.kubememlens.io/v1alpha1", "kind": "TraceAdmission", "metadata": map[string]string{"name": expected.SessionID, "namespace": "tenant"}, "state": "active", "expiresAt": deadline, "engineDigest": expected.EngineDigest})
	}))
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	server.StartTLS()
	defer server.Close()
	ca := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}))
	result, err := Connect(t.Context(), Connection{Server: server.URL, Token: "fixture-token", CAPEM: ca}, expected)
	if err != nil || !result.TransportComplete || calls.Load() != 2 {
		t.Fatal(result, err, calls.Load())
	}
	if _, err := Latencies(result); err != nil {
		t.Fatal(err)
	}

	// Another valid CA must fail TLS before the token reaches an HTTP handler.
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign, NotBefore: start.Add(-time.Hour), NotAfter: start.Add(time.Hour)}
	cert, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	wrongCA := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert}))
	if _, err := Connect(t.Context(), Connection{Server: server.URL, Token: "fixture-token", CAPEM: wrongCA}, expected); err == nil || calls.Load() != 2 {
		t.Fatal("untrusted TLS reached authenticated HTTP")
	}
}
