package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// Use a loopback listener with an EKS-shaped certificate. Only the test dialler
// maps the route locally; real TLS CA, hostname and SNI checks still run.
func TestEKSCollectorPreservesCAAndHostnameBinding(t *testing.T) {
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
	var requests atomic.Int32
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.TLS.ServerName != host || r.TLS.Version != tls.VersionTLS13 || r.Host != host || r.Header.Get("Authorization") != "Bearer fixture-token" || r.URL.Path != "/apis/memory.kubememlens.io/v1alpha1/metrics/current" {
			t.Error("provider identity or route changed")
		}
		_ = json.NewEncoder(w).Encode(metricsEnvelope())
	}))
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	server.TLS = &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}}
	server.StartTLS()
	defer server.Close()
	client, err := collectorClient("https://"+host, "fixture-token", caFor(server), "eks")
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseIdleConnections()
	tr := client.Transport.(*http.Transport)
	tr.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	}
	if _, err := readCollector(context.Background(), client, "https://"+host, "fixture-token"); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 1 {
		t.Fatal("authenticated provider route not exercised")
	}
	if _, err := readCollector(context.Background(), client, "https://other.us-east-1.eks.amazonaws.com", "fixture-token"); err == nil || requests.Load() != 1 {
		t.Fatal("wrong hostname received authenticated request")
	}
}
