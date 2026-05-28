package client

import (
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

func TestParseConnectionMode(t *testing.T) {
	for _, value := range []string{"", "auto", "http", "kube-proxy"} {
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
	if mode != ConnectionModeKubeProxy {
		t.Fatalf("mode = %s, want kube-proxy", mode)
	}
}
