package admissionkube

import (
	"context"
	"crypto/tls"
	"encoding/pem"
	"errors"
	"io"
	utilnet "k8s.io/apimachinery/pkg/util/net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type trackedBody struct {
	io.Reader
	closed bool
}

func (b *trackedBody) Close() error { b.closed = true; return nil }

func TestOversizedResponsesAreRejectedForFixedAndChunkedBodies(t *testing.T) {
	for _, length := range []int64{MaxAPIResponseBytes + 1, -1} {
		body := &trackedBody{Reader: strings.NewReader(strings.Repeat("x", MaxAPIResponseBytes+1))}
		transport := boundedTransport{next: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, ContentLength: length, Body: body}, nil
		})}
		response, err := transport.RoundTrip(httptest.NewRequest(http.MethodGet, "https://kubernetes.invalid", nil))
		if length > 0 {
			if err == nil || !body.closed {
				t.Fatal("oversized declared response accepted")
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(response.Body)
		response.Body.Close()
		var sizeError *http.MaxBytesError
		if !errors.As(err, &sizeError) || len(data) > MaxAPIResponseBytes || !body.closed {
			t.Fatal("chunked response exceeded the body ceiling")
		}
	}
}

func TestClientRequiresVerifiedTLSAndReadsThroughTheRealHTTPPath(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/namespaces/tenant-a/pods/pod" {
			t.Errorf("unexpected route %s", r.URL.Path)
			w.WriteHeader(404)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"apiVersion":"v1","kind":"Pod","metadata":{"namespace":"tenant-a","name":"pod","uid":"uid-a"}}`)
	}))
	defer server.Close()
	ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	config := &rest.Config{Host: server.URL, TLSClientConfig: rest.TLSClientConfig{CAData: ca}}
	client, err := NewClient(config)
	if err != nil {
		t.Fatal(err)
	}
	pod, err := client.CoreV1().Pods("tenant-a").Get(context.Background(), "pod", metav1.GetOptions{})
	if err != nil || pod.Name != "pod" {
		t.Fatalf("verified HTTP read: %v", err)
	}
	if config.Timeout != 0 || config.QPS != 0 {
		t.Fatal("client modified caller configuration")
	}
	for _, bad := range []*rest.Config{nil, {Host: "http://localhost"}, {Host: server.URL, TLSClientConfig: rest.TLSClientConfig{Insecure: true}}, {Host: "https://user:password@example.invalid"}} {
		if _, err := NewClient(bad); err == nil {
			t.Fatal("unverified transport accepted")
		}
	}
}

// Client-go uses transport unwrapping for cancellation and certificate
// inspection. This verifies the latter through the real wrapper interface.
func TestBoundedTransportPreservesTLSInspection(t *testing.T) {
	config := &tls.Config{MinVersion: tls.VersionTLS13}
	transport := boundedTransport{next: &http.Transport{TLSClientConfig: config}}
	actual, err := utilnet.TLSClientConfig(transport)
	if err != nil || actual != config {
		t.Fatalf("TLS inspection lost through response bound: %v", err)
	}
}
