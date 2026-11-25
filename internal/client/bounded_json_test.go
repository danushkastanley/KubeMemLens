package client

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"k8s.io/client-go/rest"
)

// oversizedCollector answers every request with valid JSON that is larger than
// the limit: leading whitespace, then an empty object.
func oversizedCollector(t *testing.T) *httptest.Server {
	t.Helper()
	padding := bytes.Repeat([]byte(" "), 1<<20)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		for written := 0; written <= legacyMaxResponseBytes; written += len(padding) {
			if _, err := w.Write(padding); err != nil {
				return
			}
		}
		_, _ = w.Write([]byte("{}"))
	}))
	t.Cleanup(server.Close)
	return server
}

func TestDirectCollectorReadIsBounded(t *testing.T) {
	server := oversizedCollector(t)
	_, err := NewCollectorClientWithTimeout(server.URL, 30*time.Second).DebugStore(context.Background())
	if !errors.Is(err, errResponseTooLarge) {
		t.Fatalf("oversized direct response: err = %v, want the byte limit error", err)
	}
}

func TestServiceProxyCollectorReadIsBounded(t *testing.T) {
	server := oversizedCollector(t)
	reader, err := NewKubeProxyCollectorClient(&rest.Config{Host: server.URL}, "kube-memlens", "kube-memlens-collector", 8080, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	_, err = reader.DebugStore(context.Background())
	if !errors.Is(err, errResponseTooLarge) {
		t.Fatalf("oversized proxied response: err = %v, want the byte limit error", err)
	}
}

func TestBoundedDecodeAcceptsExactlyTheLimit(t *testing.T) {
	body := `{"a":1}`
	var out struct {
		A int `json:"a"`
	}
	if err := decodeBoundedJSON(strings.NewReader(body), int64(len(body)), &out); err != nil || out.A != 1 {
		t.Fatalf("body of exactly the limit: err = %v, a = %d", err, out.A)
	}
	if err := decodeBoundedJSON(strings.NewReader(body), int64(len(body))-1, &out); !errors.Is(err, errResponseTooLarge) {
		t.Fatalf("body one byte over the limit: err = %v", err)
	}
	if err := decodeBoundedJSON(strings.NewReader(body+`{}`), 1024, &out); err == nil {
		t.Fatal("trailing JSON accepted")
	}
}

func TestServiceProxyReadReportsAFailedStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "collector unavailable", http.StatusServiceUnavailable)
	}))
	t.Cleanup(server.Close)
	reader, err := NewKubeProxyCollectorClient(&rest.Config{Host: server.URL}, "kube-memlens", "kube-memlens-collector", 8080, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reader.DebugStore(context.Background()); err == nil || !strings.Contains(err.Error(), "attempted service names") {
		t.Fatalf("failed proxied read: err = %v", err)
	}
}
