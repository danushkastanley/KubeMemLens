package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCollectorClientTrimsBaseURLAndFetchesPods(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()

	client := NewCollectorClient(server.URL + "/")
	pods, err := client.Pods(context.Background())
	if err != nil {
		t.Fatalf("Pods returned error: %v", err)
	}
	if len(pods) != 0 {
		t.Fatalf("pods = %d, want 0", len(pods))
	}
	if gotPath != "/api/v1/pods" {
		t.Fatalf("path = %q, want /api/v1/pods", gotPath)
	}
}

func TestCollectorClientNon200IncludesStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not ready", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	_, err := NewCollectorClient(server.URL).Namespaces(context.Background())
	if err == nil {
		t.Fatal("Namespaces returned nil error")
	}
	if !contains(err.Error(), "status 503") {
		t.Fatalf("error = %q, want status", err.Error())
	}
}

func TestCollectorClientMalformedJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`not-json`))
	}))
	defer server.Close()

	_, err := NewCollectorClient(server.URL).Containers(context.Background())
	if err == nil {
		t.Fatal("Containers returned nil error")
	}
	if !contains(err.Error(), "decode") {
		t.Fatalf("error = %q, want decode context", err.Error())
	}
}

func TestCollectorClientHealth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			t.Fatalf("path = %q, want /healthz", r.URL.Path)
		}
		_, _ = w.Write([]byte("ok\n"))
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := NewCollectorClient(server.URL).Health(ctx); err != nil {
		t.Fatalf("Health returned error: %v", err)
	}
}

func contains(value string, needle string) bool {
	return strings.Contains(value, needle)
}
