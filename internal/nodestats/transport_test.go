package nodestats

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/nodecontext"
)

func TestResponseFailuresAreTypedAndBounded(t *testing.T) {
	for _, test := range []struct {
		name     string
		status   int
		body     string
		encoding string
		reason   nodecontext.Reason
	}{
		{"auth", 401, "private-server-error", "", nodecontext.Authentication},
		{"denied", 403, "private-server-error", "", nodecontext.Forbidden},
		{"limited", 429, "", "", nodecontext.Throttled},
		{"absent", 404, "", "", nodecontext.Unsupported},
		{"failed", 500, "", "", nodecontext.SourceUnavailable},
		{"malformed", 200, `{"node":`, "", nodecontext.InvalidResponse},
		{"compressed", 200, summaryBody(), "gzip", nodecontext.InvalidResponse},
		{"oversized", 200, strings.Repeat("x", nodecontext.MaxSummaryBytes+2), "", nodecontext.ResponseTooLarge},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Content-Encoding", test.encoding)
				w.WriteHeader(test.status)
				_, _ = fmt.Fprint(w, test.body)
			})
			report, err := h.source.Read(t.Context())
			assertReason(t, err, test.reason)
			if report.Stats != nil || strings.Contains(err.Error(), "private-") || strings.Contains(err.Error(), h.kubelet.URL) {
				t.Fatal("failed response leaked data")
			}
		})
	}
}

func TestRedirectNeverForwardsCredentials(t *testing.T) {
	var forwarded atomic.Int32
	target, _ := tlsServer(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { forwarded.Add(1) }), "127.0.0.1", time.Now().Add(time.Hour))
	h := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	})
	_, err := h.source.Read(t.Context())
	assertReason(t, err, nodecontext.InvalidTarget)
	if forwarded.Load() != 0 {
		t.Fatal("followed redirect")
	}
}

func TestTokenRotationReloadsOnlyTheKubeletCredential(t *testing.T) {
	var expected atomic.Value
	expected.Store("Bearer node-credential")
	h := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != expected.Load().(string) {
			t.Error("stale or API credential reached kubelet")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, summaryBody())
	})
	if _, err := h.source.Read(t.Context()); err != nil {
		t.Fatal(err)
	}
	expected.Store("Bearer rotated-credential")
	if err := os.WriteFile(h.opts.TokenFile, []byte("rotated-credential"), 0600); err != nil {
		t.Fatal(err)
	}
	h.now = h.now.Add(nodecontext.CollectionInterval)
	if _, err := h.source.Read(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestCancellationTimeoutAndRequestSpacing(t *testing.T) {
	entered := make(chan struct{}, 1)
	h := newHarness(t, func(w http.ResponseWriter, r *http.Request) { entered <- struct{}{}; <-r.Context().Done() })
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { _, err := h.source.Read(ctx); done <- err }()
	<-entered
	_, concurrent := h.source.Read(t.Context())
	assertReason(t, concurrent, nodecontext.Throttled)
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
	_, frequent := h.source.Read(t.Context())
	assertReason(t, frequent, nodecontext.Throttled)
	h.now = h.now.Add(nodecontext.CollectionInterval)
	h.opts.Timeout = 20 * time.Millisecond
	source, err := New(h.config, h.opts)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	_, err = source.Read(t.Context())
	assertReason(t, err, nodecontext.TimedOut)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("deadline cause lost")
	}
}

func TestWrongCARejectsBeforeBearerTransmission(t *testing.T) {
	var received atomic.Int32
	h := newHarness(t, func(http.ResponseWriter, *http.Request) { received.Add(1) })
	if err := os.WriteFile(h.opts.CAFile, h.config.CAData, 0600); err != nil {
		t.Fatal(err)
	}
	_, err := h.source.Read(t.Context())
	assertReason(t, err, nodecontext.UntrustedTLS)
	if received.Load() != 0 {
		t.Fatal("sent bearer to an untrusted endpoint")
	}
}

func TestCredentialAndCABoundsFailClosed(t *testing.T) {
	for _, test := range []struct {
		name   string
		file   string
		data   string
		reason nodecontext.Reason
	}{
		{"long-token", "token", strings.Repeat("t", (16<<10)+1), nodecontext.Authentication},
		{"empty-token", "token", "", nodecontext.Authentication},
		{"newline-token", "token", "first\nsecond", nodecontext.Authentication},
		{"invalid-ca", "ca", "private-ca-error", nodecontext.UntrustedTLS},
		{"long-ca", "ca", strings.Repeat("c", (1<<20)+1), nodecontext.UntrustedTLS},
	} {
		t.Run(test.name, func(t *testing.T) {
			var calls atomic.Int32
			h := newHarness(t, func(http.ResponseWriter, *http.Request) { calls.Add(1) })
			path := h.opts.TokenFile
			if test.file == "ca" {
				path = h.opts.CAFile
			}
			if err := os.WriteFile(path, []byte(test.data), 0600); err != nil {
				t.Fatal(err)
			}
			_, err := h.source.Read(t.Context())
			assertReason(t, err, test.reason)
			if calls.Load() != 0 || strings.Contains(err.Error(), path) {
				t.Fatal("invalid credential sent or path disclosed")
			}
		})
	}
}

func TestMetricsDoNotRetainUnknownReasonsOrIdentity(t *testing.T) {
	telemetry := &Telemetry{}
	telemetry.record(&Error{Reason: "private-node-name", cause: errors.New("private-token")}, time.Second, 100)
	text := telemetry.Render()
	if strings.Contains(text, "private-") || !strings.Contains(text, `result="invalid-response"} 1`) {
		t.Fatal(text)
	}
}

func assertReason(t *testing.T, err error, reason nodecontext.Reason) {
	t.Helper()
	var failure *Error
	if !errors.As(err, &failure) || failure.Reason != reason {
		t.Fatalf("error=%v, want reason=%s", err, reason)
	}
}
