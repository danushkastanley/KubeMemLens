package delivery

import (
	"context"
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
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestEKSDeliveryPreservesHostnameAndAuthenticatedRoutes(t *testing.T) {
	const host = "owned.us-east-1.eks.amazonaws.com"
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	cert := &x509.Certificate{SerialNumber: big.NewInt(1), DNSNames: []string{host},
		NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	data, expected, deadline, now := fixture(t, fixturePath)
	var calls atomic.Int32
	path := "/apis/tracing.kubememlens.io/v1alpha1/namespaces/tenant/traces/" + expected.SessionID
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.TLS.ServerName != host || r.TLS.Version != tls.VersionTLS13 || r.Host != host || r.Header.Get("Authorization") != "Bearer fixture-token" || r.Method != "GET" || (r.URL.Path != path && r.URL.Path != path+"/stream") {
			t.Error("provider identity or route changed")
		}
		if strings.HasSuffix(r.URL.Path, "/stream") {
			_, _ = w.Write(data)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"apiVersion": "tracing.kubememlens.io/v1alpha1", "kind": "TraceAdmission", "metadata": map[string]string{"name": expected.SessionID, "namespace": "tenant"}, "state": "active", "expiresAt": deadline, "engineDigest": expected.EngineDigest})
	}))
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	server.TLS = &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}}
	server.StartTLS()
	defer server.Close()
	ca := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	client, err := newHTTPClient(Connection{Server: "https://" + host, Token: "fixture-token", CAPEM: ca, NetworkScope: "eks"})
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseIdleConnections()
	client.Transport.(*http.Transport).DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	}
	result, err := readWithClient(t.Context(), client, "https://"+host, "fixture-token", expected, now)
	if err != nil || !result.TransportComplete || calls.Load() != 2 {
		t.Fatal("provider delivery failed", err, calls.Load())
	}
	if _, err := readWithClient(t.Context(), client, "https://other.us-east-1.eks.amazonaws.com", "fixture-token", expected, now); err == nil || calls.Load() != 2 {
		t.Fatal("wrong hostname received authenticated request")
	}
}
