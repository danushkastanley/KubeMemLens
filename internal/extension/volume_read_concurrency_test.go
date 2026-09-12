package extension

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/collector"
	"github.com/danushkastanley/kube-memlens/internal/kube"
)

func TestVolumeReadDoesNotHoldMemoryGate(t *testing.T) {
	h := NewReadHandler(collector.NewStore(), collector.DefaultHandlerOptions(time.Minute))
	h.volumeWorkloadsEnabled = true
	h.volumeNamespaces = map[string]bool{"team-a": true}
	entered := make(chan struct{}, 2)
	h.volumeResolver = workloadResolverFunc(func(ctx context.Context, _, _, _ string) (kube.ResolvedWorkloadVolumes, error) {
		entered <- struct{}{}
		<-ctx.Done()
		return kube.ResolvedWorkloadVolumes{}, ctx.Err()
	})
	request := func() *http.Request {
		r := readRequest(t, "/apis/memory.kubememlens.io/v1alpha1/namespaces/team-a/workloads/app/volumes?kind=deployment", true)
		r.Header.Set(api.SnapshotSchemaHeader, "6")
		return r
	}
	for range 2 {
		r := request()
		// Preserve the authenticated request context while attaching cancellation.
		ctx, cancel := context.WithCancel(r.Context())
		finished := make(chan struct{})
		go func() {
			defer close(finished)
			h.ServeHTTP(httptest.NewRecorder(), r.WithContext(ctx))
		}()
		select {
		case <-entered:
		case <-time.After(time.Second):
			cancel()
			t.Fatal("volume resolver was not entered")
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, request())
		if w.Code != http.StatusTooManyRequests {
			cancel()
			t.Fatalf("concurrent volume read: %d", w.Code)
		}
		memoryFinished := make(chan int, 1)
		memory := readRequest(t, "/apis/memory.kubememlens.io/v1alpha1/namespaces/team-a/pods", true)
		go func() {
			w := httptest.NewRecorder()
			h.ServeHTTP(w, memory)
			memoryFinished <- w.Code
		}()
		select {
		case code := <-memoryFinished:
			if code != http.StatusOK {
				t.Errorf("memory read: %d", code)
			}
		case <-time.After(time.Second):
			cancel()
			t.Fatal("volume query blocked memory read")
		}
		cancel()
		select {
		case <-finished:
		case <-time.After(time.Second):
			t.Fatal("cancelled query retained its slot")
		}
	}
}
