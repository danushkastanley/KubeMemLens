package client

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestDefaultOptionsFromEnv(t *testing.T) {
	t.Setenv("MEMLENS_COLLECTOR_URL", "http://127.0.0.1:18080")
	t.Setenv("MEMLENS_COLLECTOR_NAMESPACE", "memlens")
	t.Setenv("MEMLENS_COLLECTOR_SERVICE", "collector")
	t.Setenv("MEMLENS_COLLECTOR_PORT", "9090")

	opts := DefaultOptions()
	if opts.CollectorURL != "http://127.0.0.1:18080" {
		t.Fatalf("CollectorURL = %q", opts.CollectorURL)
	}
	if opts.CollectorNamespace != "memlens" {
		t.Fatalf("CollectorNamespace = %q", opts.CollectorNamespace)
	}
	if opts.CollectorService != "collector" {
		t.Fatalf("CollectorService = %q", opts.CollectorService)
	}
	if opts.CollectorPort != 9090 {
		t.Fatalf("CollectorPort = %d", opts.CollectorPort)
	}
	if opts.Timeout != 5*time.Second {
		t.Fatalf("Timeout = %s", opts.Timeout)
	}
}

func TestConnectionErrorPreservesReadFailureKinds(t *testing.T) {
	opts := Options{Mode: ConnectionModeKubernetesAPI}
	for _, test := range []struct {
		name string
		err  error
		want string
	}{
		{name: "forbidden", err: &ReadError{Kind: ReadErrorForbidden}, want: "do not have permission"},
		{name: "not found", err: &ReadError{Kind: ReadErrorNotFound}, want: "not found in the authorised scope"},
		{name: "unavailable", err: &ReadError{Kind: ReadErrorUnavailable, Cause: errors.New("tenant-secret")}, want: "kubectl get apiservice"},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := ConnectionError(opts, "", test.err)
			if !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %q, want %q", err, test.want)
			}
			if strings.Contains(err.Error(), "tenant-secret") {
				t.Fatalf("error exposed wrapped cause: %v", err)
			}
		})
	}
}

func TestParseConnectionMode(t *testing.T) {
	for _, value := range []string{"", "auto", "kubernetes-api", "http", "kube-proxy"} {
		if _, err := ParseConnectionMode(value); err != nil {
			t.Fatalf("ParseConnectionMode(%q) returned error: %v", value, err)
		}
	}
	if _, err := ParseConnectionMode("bad"); err == nil {
		t.Fatal("ParseConnectionMode returned nil error for bad mode")
	}
}

func TestResolveMode(t *testing.T) {
	mode, err := ResolveMode(Options{Mode: ConnectionModeAuto, CollectorURL: "http://collector"})
	if err != nil {
		t.Fatalf("ResolveMode returned error: %v", err)
	}
	if mode != ConnectionModeHTTP {
		t.Fatalf("mode = %s, want http", mode)
	}

	mode, err = ResolveMode(Options{Mode: ConnectionModeAuto})
	if err != nil {
		t.Fatalf("ResolveMode returned error: %v", err)
	}
	if mode != ConnectionModeKubernetesAPI {
		t.Fatalf("mode = %s, want kubernetes-api", mode)
	}
}
