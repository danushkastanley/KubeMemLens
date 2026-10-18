package extension

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/memoryhistory"
	"github.com/danushkastanley/kube-memlens/internal/replicabaseline"
)

func TestReplicaBusyQueryDoesNotOccupyOrdinaryLiveReadGate(t *testing.T) {
	h, _ := replicaReadFixture(t)
	entered, release := make(chan struct{}), make(chan struct{})
	original := h.replicas.evidence
	h.replicas.evidence = func(ctx context.Context, s replicabaseline.Selection, now time.Time) (replicabaseline.Input, error) {
		close(entered)
		select {
		case <-release:
			return original(ctx, s, now)
		case <-ctx.Done():
			return replicabaseline.Input{}, ctx.Err()
		}
	}
	req := readRequest(t, replicaPath, true)
	ctx, cancel := context.WithCancel(req.Context())
	defer cancel()
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { w := httptest.NewRecorder(); h.ServeHTTP(w, req.WithContext(ctx)); done <- w }()
	<-entered
	if got := serveRead(t, h, replicaPath).Code; got != http.StatusTooManyRequests {
		t.Fatal("second comparison admitted", got)
	}
	live := make(chan int, 1)
	go func() {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, readRequest(t, "/apis/memory.kubememlens.io/v1alpha1/namespaces/team-a/pods/api", true))
		live <- w.Code
	}()
	select {
	case code := <-live:
		if code != 200 {
			t.Fatal("live read failed", code)
		}
	case <-time.After(time.Second):
		t.Fatal("comparison blocked live read")
	}
	cancel()
	if result := <-done; result.Code != http.StatusServiceUnavailable {
		t.Fatal("cancelled comparison returned evidence", result.Code)
	}
	if len(h.replicas.gate) != 0 {
		t.Fatal("cancelled request retained gate")
	}
	close(release)
}

func TestReplicaFreshnessIsEvaluatedAfterMetadataRevalidation(t *testing.T) {
	h, s := replicaReadFixture(t)
	now := h.now()
	h.now = func() time.Time { return now }
	calls := 0
	h.replicas.resolver = replicaResolveFunc(func(context.Context, memoryhistory.Request) (replicabaseline.Selection, error) {
		calls++
		if calls == 2 {
			now = now.Add(31 * time.Second)
		}
		s.ResolvedAt = now
		return s, nil
	})
	w := serveRead(t, h, replicaPath)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var body replicaResource
	if json.Unmarshal(w.Body.Bytes(), &body) != nil {
		t.Fatal("invalid response")
	}
	for _, peer := range body.Baseline.Peers {
		if peer.Exclusion != "source-stale" || len(peer.Comparisons) != 0 {
			t.Fatal("revalidation latency made stale data appear current")
		}
	}
}
