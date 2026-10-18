package promhistory

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/memoryhistory"
)

func TestBoundedAuthenticatedQueryAndZeroSamples(t *testing.T) {
	s := fixtureSelection()
	q := fixtureQuery(s)
	c := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/api/v1/query_range" || r.Header.Get("Authorization") != "Bearer test-token" {
			t.Error("transport contract")
		}
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		expression := r.Form.Get("query")
		for _, want := range []string{`pod_uid="pod-uid"`, `container_id="containerd://current"`, `node_uid="node-uid"`, `namespace="tenant-a"`, `cluster="cluster-a"`, `timestamp(`} {
			if !strings.Contains(expression, want) {
				t.Errorf("missing fixed constraint %s", want)
			}
		}
		if r.Form.Get("limit") != "33" || r.Form.Get("timeout") != "4s" || r.Form.Get("step") != "60" {
			t.Error("server query bounds")
		}
		respond(w, fixtureResponse(s, q))
	})
	r, err := c.Query(t.Context(), s, q)
	if err != nil {
		t.Fatal(err)
	}
	if r.Completeness != memoryhistory.Complete || r.State != memoryhistory.Fresh || len(r.Series) != 1 || len(r.Series[0].Points) != 3 {
		t.Fatalf("report: %+v", r)
	}
	for _, p := range r.Series[0].Points {
		if p.Bytes == nil || *p.Bytes != 0 || p.State != memoryhistory.Fresh || !p.At.Equal(p.SampledAt) {
			t.Fatal("zero sample or source timestamp lost")
		}
	}
}

func TestGapsStaleSamplesAndWarnings(t *testing.T) {
	s := fixtureSelection()
	q := fixtureQuery(s)
	response := fixtureResponse(s, q)
	response.Warnings = []string{"backend contains private details"}
	for i := range response.Data.Result {
		response.Data.Result[i].Values = append(response.Data.Result[i].Values[:1], response.Data.Result[i].Values[2:]...)
	}
	stamp, _ := json.Marshal(q.End.Add(-3 * time.Minute).Unix())
	response.Data.Result[1].Values[1][1] = json.RawMessage(`"` + string(stamp) + `"`)
	c := fixtureClient(t, func(w http.ResponseWriter, _ *http.Request) { respond(w, response) })
	r, err := c.Query(t.Context(), s, q)
	if err != nil {
		t.Fatal(err)
	}
	points := r.Series[0].Points
	if r.Completeness != memoryhistory.Partial || r.Reason != "provider-partial" || points[1].Bytes != nil || points[1].State != memoryhistory.Missing || points[2].State != memoryhistory.Stale {
		t.Fatal("partial evidence semantics lost")
	}
	wire, _ := json.Marshal(r)
	if strings.Contains(string(wire), "private details") {
		t.Fatal("provider warning leaked")
	}
}

func TestProviderFailureDoesNotReturnPartialValues(t *testing.T) {
	s := fixtureSelection()
	q := fixtureQuery(s)
	for _, status := range []int{http.StatusForbidden, http.StatusTooManyRequests, http.StatusServiceUnavailable, http.StatusFound} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			c := fixtureClient(t, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Location", "https://example.invalid")
				w.WriteHeader(status)
				_, _ = w.Write([]byte("private server detail"))
			})
			r, err := c.Query(t.Context(), s, q)
			if !errors.Is(err, memoryhistory.ErrUnavailable) || len(r.Series) != 0 || strings.Contains(err.Error(), "private") {
				t.Fatalf("failure contract: %v", err)
			}
		})
	}
}

func TestCancellationAndConcurrentQueryBudget(t *testing.T) {
	s := fixtureSelection()
	q := fixtureQuery(s)
	entered := make(chan struct{})
	c := fixtureClient(t, func(_ http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Error(err)
			return
		}
		close(entered)
		<-r.Context().Done()
	})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := c.Query(ctx, s, q); done <- err }()
	<-entered
	if _, err := c.Query(t.Context(), s, q); !errors.Is(err, memoryhistory.ErrBounds) {
		t.Fatalf("concurrent provider query: %v", err)
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
}
