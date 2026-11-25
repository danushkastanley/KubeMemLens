package main

import (
	"flag"
	"io"
	"testing"
)

func parsedIngestionMode(t *testing.T, args ...string) string {
	t.Helper()
	flags := flag.NewFlagSet("collector", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	mode := registerIngestionMode(flags)
	if err := flags.Parse(args); err != nil {
		t.Fatal(err)
	}
	return *mode
}

// A collector started with no flags must not serve unauthenticated reads and
// writes. The unauthenticated mode has to be asked for.
func TestCollectorServesUnauthenticatedHTTPOnlyWhenAsked(t *testing.T) {
	if mode := parsedIngestionMode(t); mode != ingestionAuthenticated {
		t.Fatalf("ingestion mode with no flags = %q, want %q", mode, ingestionAuthenticated)
	}
	if mode := parsedIngestionMode(t, "--ingestion-mode=legacy"); mode != ingestionLegacy {
		t.Fatalf("explicit legacy mode = %q", mode)
	}
}

func TestAuthenticatedModeNeedsServingCertificateFiles(t *testing.T) {
	for _, tc := range []struct {
		mode, cert, key string
		ok              bool
	}{
		{ingestionAuthenticated, "", "", false},
		{ingestionAuthenticated, "/tls/tls.crt", "", false},
		{ingestionAuthenticated, "/tls/tls.crt", "/tls/tls.key", true},
		{ingestionLegacy, "", "", true},
	} {
		if err := checkIngestionMode(tc.mode, tc.cert, tc.key); (err == nil) != tc.ok {
			t.Fatalf("mode %q cert %q key %q: err = %v", tc.mode, tc.cert, tc.key, err)
		}
	}
	if checkIngestionMode("plaintext", "", "") == nil {
		t.Fatal("unknown mode accepted")
	}
}
