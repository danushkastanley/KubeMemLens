package nodestats

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/nodecontext"
)

func TestSANAndExpiryFailuresDoNotTransmitBearer(t *testing.T) {
	for _, test := range []struct {
		name, ip string
		expiry   time.Time
	}{
		{"wrong-san", "127.0.0.2", time.Now().Add(time.Hour)},
		{"expired", "127.0.0.1", time.Now().Add(-time.Hour)},
	} {
		t.Run(test.name, func(t *testing.T) {
			var received atomic.Int32
			server, ca := tlsServer(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { received.Add(1) }), test.ip, test.expiry)
			h := newHarness(t, successHandler(t))
			if err := os.WriteFile(h.opts.CAFile, ca, 0600); err != nil {
				t.Fatal(err)
			}
			client, token, err := h.source.kubeletClient()
			if err != nil {
				t.Fatal(err)
			}
			defer client.CloseIdleConnections()
			_, err = getBytes(t.Context(), client, server.URL+"/stats/summary", token, nodecontext.MaxSummaryBytes)
			assertReason(t, err, nodecontext.UntrustedTLS)
			if received.Load() != 0 {
				t.Fatal("bearer reached invalid serving identity")
			}
		})
	}
}

func TestCARotationIsObservedWithoutRestart(t *testing.T) {
	h := newHarness(t, successHandler(t))
	original, err := os.ReadFile(h.opts.CAFile)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.source.Read(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(h.opts.CAFile, h.config.CAData, 0600); err != nil {
		t.Fatal(err)
	}
	h.now = h.now.Add(nodecontext.CollectionInterval)
	_, err = h.source.Read(t.Context())
	assertReason(t, err, nodecontext.UntrustedTLS)
	if err := os.WriteFile(h.opts.CAFile, original, 0600); err != nil {
		t.Fatal(err)
	}
	h.now = h.now.Add(nodecontext.CollectionInterval)
	if _, err := h.source.Read(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestErrorEncodingOmitsPrivateTransportCause(t *testing.T) {
	value := &Error{Reason: nodecontext.Unreachable, cause: errors.New("private-node-url private-token")}
	data, err := json.Marshal(value)
	if err != nil || strings.Contains(string(data), "private-") || strings.Contains(value.Error(), "private-") {
		t.Fatalf("cause disclosed: %s", data)
	}
}

func TestSourceRejectsInsecureAPIConfiguration(t *testing.T) {
	h := newHarness(t, successHandler(t))
	config := *h.config
	config.Insecure = true
	_, err := New(&config, h.opts)
	assertReason(t, err, nodecontext.InvalidTarget)
	config = *h.config
	config.Host = strings.Replace(config.Host, "https://", "http://", 1)
	_, err = New(&config, h.opts)
	assertReason(t, err, nodecontext.InvalidTarget)
}
