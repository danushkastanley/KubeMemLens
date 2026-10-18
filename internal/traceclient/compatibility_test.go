package traceclient

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	"github.com/danushkastanley/kube-memlens/internal/tracecompat"
)

func TestUnversionedExtensionFailsBeforeAdmissionOrActivation(t *testing.T) {
	mutations := 0
	c, _ := unversionedFixtureClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(tracecompat.Header) != tracecompat.Offer {
			t.Error("offer absent")
		}
		if r.URL.Path != apiPrefix {
			mutations++
			t.Error("incompatible server reached target operation")
			w.WriteHeader(500)
			return
		}
		discoverFixture(w)
	}))
	_, err := c.Preflight(context.Background(), selectionFixture(), DefaultIntent(trace.Files))
	var failure *Error
	if !errors.As(err, &failure) || failure.Kind != Incompatible || mutations != 0 {
		t.Fatalf("incompatibility not explicit: %v, operations %d", err, mutations)
	}
}
func TestUnsupportedContractAndMissingStreamAckAreExplicit(t *testing.T) {
	for _, response := range []string{"absent", "future", "duplicate", "declined"} {
		t.Run(response, func(t *testing.T) {
			c, _ := unversionedFixtureClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.RawQuery != tracecompat.StreamQuery || r.Header.Get(tracecompat.Header) != tracecompat.Offer {
					t.Error("activation can reach a legacy handler without rejection")
				}
				switch response {
				case "future":
					w.Header().Set(tracecompat.Header, "2")
				case "duplicate":
					w.Header().Add(tracecompat.Header, "1")
					w.Header().Add(tracecompat.Header, "1")
				case "declined":
					w.WriteHeader(406)
					return
				}
				w.WriteHeader(200)
			}))
			a := Admission{client: c, namespace: "tenant-a", id: strings.Repeat("a", 32)}
			result, err := c.Watch(context.Background(), a, nil)
			var failure *Error
			if !errors.As(err, &failure) || failure.Kind != Incompatible || result.TransportComplete || result.DeliveredEvents != 0 {
				t.Fatalf("unexpected incompatible stream result: %v", err)
			}
		})
	}
}
func TestCancellationRemainsAvailableAcrossContractMismatch(t *testing.T) {
	c, _ := unversionedFixtureClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || len(r.Header.Values(tracecompat.Header)) != 0 {
			t.Error("cancel unexpectedly requires negotiation")
		}
		_, _ = w.Write([]byte(`{"apiVersion":"v1","kind":"Status","status":"Success","code":200}`))
	}))
	cleanup, err := c.CancelID(context.Background(), "tenant-a", strings.Repeat("a", 32))
	if err != nil || cleanup != CleanupConfirmed {
		t.Fatalf("stable cancellation rejected: %v", err)
	}
}

func TestRollingDowngradeRefusesActivationBeforeLegacyHandlerStarts(t *testing.T) {
	activations := 0
	c, _ := unversionedFixtureClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// This is the previous handler's actual pre-claim query guard. The live
		// old-binary matrix remains a separate live verification requirement.
		if r.URL.RawQuery != "" {
			w.WriteHeader(400)
			return
		}
		activations++
		w.WriteHeader(200)
	}))
	_, err := c.Watch(context.Background(), Admission{client: c, namespace: "tenant-a", id: strings.Repeat("a", 32)}, nil)
	var failure *Error
	if !errors.As(err, &failure) || failure.Kind != Incompatible || activations != 0 {
		t.Fatalf("legacy activation occurred or error was misleading: %v", err)
	}
}
func TestCorePodReadsDoNotNegotiateTraceContracts(t *testing.T) {
	c, _ := unversionedFixtureClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(r.Header.Values(tracecompat.Header)) != 0 {
			t.Error("trace contract sent to Kubernetes core API")
		}
		w.WriteHeader(403)
	}))
	_, err := c.Select(context.Background(), "tenant-a", "pod", "worker")
	var failure *Error
	if !errors.As(err, &failure) || failure.Kind != Denied {
		t.Fatalf("Pod permission boundary changed: %v", err)
	}
}
