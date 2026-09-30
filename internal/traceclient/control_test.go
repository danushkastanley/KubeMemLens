package traceclient

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
)

func TestPreflightAndCreateRetainReviewedSelection(t *testing.T) {
	doc := preflightFixture(t, selectionFixture(), DefaultIntent(trace.Files))
	var creates atomic.Int64
	c, _ := fixtureClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fixture-token" || r.URL.RawQuery != "" {
			t.Error("credential or bounded raw URL changed")
		}
		switch r.URL.Path {
		case apiPrefix:
			discoverFixture(w)
		case apiPrefix + "/namespaces/tenant-a/tracepreflights":
			_ = json.NewEncoder(w).Encode(doc)
		case apiPrefix + "/namespaces/tenant-a/traces":
			creates.Add(1)
			data, _ := io.ReadAll(r.Body)
			var body map[string]any
			_ = json.Unmarshal(data, &body)
			if body["schemaVersion"] != float64(3) || body["contractVersion"] != float64(1) || body["expectedPodUID"] != "selected-uid" || body["expectedContainerID"] != strings.Repeat("a", 64) {
				t.Error("selection precondition lost")
			}
			w.WriteHeader(201)
			_ = json.NewEncoder(w).Encode(admittedFixture("tenant-a", strings.Repeat("c", 32)))
		default:
			http.NotFound(w, r)
		}
	}))
	p := selectedPlan(t, c)
	copy := p.PreflightReport()
	copy.Node.Baseline.Checks[0].Value = "changed"
	copy.Node.EngineDigest = "changed"
	a, err := c.Create(context.Background(), p)
	if err != nil || a.ID() != strings.Repeat("c", 32) || creates.Load() != 1 || p.PreflightReport().Node.Baseline.Checks[0].Value != "" {
		t.Fatal("reviewed plan changed", err)
	}
}

func TestLostOrMalformedCreateIsUncertainAndNeverReplayed(t *testing.T) {
	for _, scenario := range []string{"lost", "malformed", "server-error"} {
		t.Run(scenario, func(t *testing.T) {
			var calls atomic.Int64
			c, _ := fixtureClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				switch scenario {
				case "lost":
					panic(http.ErrAbortHandler)
				case "malformed":
					w.WriteHeader(201)
					_, _ = io.WriteString(w, `{"kind":"TraceAdmission"}`)
				case "server-error":
					http.Error(w, "private detail", 503)
				}
			}))
			p := planFixture(t, c, preflightFixture(t, selectionFixture(), DefaultIntent(trace.Files)))
			_, err := c.Create(context.Background(), p)
			var failure *Error
			if !errors.As(err, &failure) || failure.Kind != Uncertain || calls.Load() != 1 || strings.Contains(err.Error(), "private detail") {
				t.Fatal("ambiguous create retried or hidden", err, calls.Load())
			}
		})
	}
}

func TestCancellationRequiresPositiveCleanupResponse(t *testing.T) {
	for _, status := range []int{200, 403, 404, 410, 503} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			c, _ := fixtureClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "DELETE" || r.URL.Path != apiPrefix+"/namespaces/tenant-a/traces/"+strings.Repeat("c", 32) {
					t.Error("cancel target changed")
				}
				w.WriteHeader(status)
				if status == 200 {
					_ = json.NewEncoder(w).Encode(metav1.Status{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Status"}, Status: metav1.StatusSuccess, Code: 200})
				}
			}))
			cleanup, err := c.CancelID(context.Background(), "tenant-a", strings.Repeat("c", 32))
			if (status == 200) != (cleanup == CleanupConfirmed && err == nil) {
				t.Fatal("cancellation confirmation changed", cleanup, err)
			}
			if status != 200 && cleanup != CleanupUnconfirmed {
				t.Fatal("absence or rejection became confirmed cleanup")
			}
		})
	}
}

func TestClientRejectsUnverifiedOrAmbiguousEndpoints(t *testing.T) {
	for _, cfg := range []*rest.Config{nil, {Host: "http://localhost"}, {Host: "https://localhost", TLSClientConfig: rest.TLSClientConfig{Insecure: true}}, {Host: "https://user:secret@localhost"}, {Host: "https://localhost?other=target"}, {Host: "https://localhost#other"}} {
		if _, err := New(cfg); err == nil {
			t.Fatal("unsafe endpoint accepted")
		}
	}
}
