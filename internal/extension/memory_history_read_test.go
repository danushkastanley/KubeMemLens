package extension

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/memoryhistory"
)

type historyResolveFunc func(context.Context, memoryhistory.Request) (memoryhistory.Selection, error)

func (f historyResolveFunc) Resolve(ctx context.Context, r memoryhistory.Request) (memoryhistory.Selection, error) {
	return f(ctx, r)
}

type historyProviderFunc func(context.Context, memoryhistory.Selection, memoryhistory.Query) (memoryhistory.Report, error)

func (f historyProviderFunc) Query(ctx context.Context, s memoryhistory.Selection, q memoryhistory.Query) (memoryhistory.Report, error) {
	return f(ctx, s, q)
}

func historyReadFixture(t *testing.T) (*ReadHandler, memoryhistory.Selection) {
	t.Helper()
	h, now := populatedReadHandler(t)
	s := memoryhistory.Selection{Request: memoryhistory.Request{Scope: memoryhistory.Pod, Namespace: "team-a", Name: "api"}, UID: "pod-uid", ResolvedAt: now,
		Targets: []memoryhistory.Target{{Namespace: "team-a", Pod: "api", PodUID: "pod-uid", Container: "app", ContainerID: "containerd://current", Node: "node-a", NodeUID: "node-uid", StartedAt: now.Add(-time.Hour), PodCreatedAt: now.Add(-time.Hour)}}}
	provider := historyProviderFunc(func(_ context.Context, s memoryhistory.Selection, q memoryhistory.Query) (memoryhistory.Report, error) {
		return memoryhistory.NewReport(s, q, now)
	})
	h.memoryHistory = &memoryHistoryService{resolver: historyResolveFunc(func(context.Context, memoryhistory.Request) (memoryhistory.Selection, error) { return s, nil }), local: provider, remote: provider, namespaces: map[string]bool{"team-a": true}, gate: make(chan struct{}, 1)}
	return h, s
}

func TestMemoryHistoryDoesNotAcquireBeforeScopeAndAuthorisation(t *testing.T) {
	for _, path := range []string{
		"/apis/memory.kubememlens.io/v1alpha1/namespaces/team-b/pods/api/trends",
		"/apis/memory.kubememlens.io/v1alpha1/namespaces/team-a/pods/api/trends?query=up",
		"/apis/memory.kubememlens.io/v1alpha1/namespaces/team-a/pods/api/trends?source=prometheus&source=local",
		"/apis/memory.kubememlens.io/v1alpha1/namespaces/team-a/pods/api/trends?step=0",
		"/apis/memory.kubememlens.io/v1alpha1/nodes/node-a/trends",
		"/apis/memory.kubememlens.io/v1alpha1/namespaces/team-a/pods/api/trends?container=%zz",
		"/apis/memory.kubememlens.io/v1alpha1/namespaces/team-a/pods/api/trends?source=prometheus;ignored",
	} {
		t.Run(path, func(t *testing.T) {
			h, _ := historyReadFixture(t)
			calls := 0
			h.memoryHistory.resolver = historyResolveFunc(func(context.Context, memoryhistory.Request) (memoryhistory.Selection, error) {
				calls++
				return memoryhistory.Selection{}, memoryhistory.ErrDenied
			})
			response := serveRead(t, h, path)
			if response.Code == http.StatusOK || calls != 0 {
				t.Fatal("invalid or out-of-scope request acquired evidence")
			}
		})
	}
	h, _ := historyReadFixture(t)
	h.memoryHistory.resolver = historyResolveFunc(func(context.Context, memoryhistory.Request) (memoryhistory.Selection, error) {
		return memoryhistory.Selection{}, memoryhistory.ErrDenied
	})
	h.memoryHistory.remote = historyProviderFunc(func(context.Context, memoryhistory.Selection, memoryhistory.Query) (memoryhistory.Report, error) {
		t.Error("denied query reached provider")
		return memoryhistory.Report{}, nil
	})
	response := serveRead(t, h, "/apis/memory.kubememlens.io/v1alpha1/namespaces/team-a/pods/api/trends?source=prometheus")
	if response.Code != http.StatusForbidden {
		t.Fatal(response.Code)
	}
}

func TestMemoryHistoryDiscardsEvidenceAfterReplacementOrRevocation(t *testing.T) {
	for _, revoked := range []bool{false, true} {
		h, selected := historyReadFixture(t)
		calls := 0
		h.memoryHistory.resolver = historyResolveFunc(func(context.Context, memoryhistory.Request) (memoryhistory.Selection, error) {
			calls++
			if calls == 2 {
				if revoked {
					return memoryhistory.Selection{}, memoryhistory.ErrDenied
				}
				selected.UID = "replacement"
			}
			return selected, nil
		})
		response := serveRead(t, h, "/apis/memory.kubememlens.io/v1alpha1/namespaces/team-a/pods/api/trends?source=prometheus")
		want := http.StatusConflict
		if revoked {
			want = http.StatusForbidden
		}
		if response.Code != want || calls != 2 {
			t.Fatalf("post-query validation: status=%d calls=%d", response.Code, calls)
		}
	}
}

func TestSlowHistoryDoesNotBlockLiveReads(t *testing.T) {
	h, _ := historyReadFixture(t)
	entered, release := make(chan struct{}), make(chan struct{})
	h.memoryHistory.remote = historyProviderFunc(func(ctx context.Context, s memoryhistory.Selection, q memoryhistory.Query) (memoryhistory.Report, error) {
		close(entered)
		select {
		case <-release:
			return memoryhistory.NewReport(s, q, h.now())
		case <-ctx.Done():
			return memoryhistory.Report{}, ctx.Err()
		}
	})
	path := "/apis/memory.kubememlens.io/v1alpha1/namespaces/team-a/pods/api/trends?source=prometheus"
	request := readRequest(t, path, true)
	finished := make(chan *httptest.ResponseRecorder, 1)
	go func() { response := httptest.NewRecorder(); h.ServeHTTP(response, request); finished <- response }()
	<-entered
	if response := serveRead(t, h, path); response.Code != http.StatusTooManyRequests {
		close(release)
		t.Fatal("second history request was not rejected")
	}
	liveRequest := readRequest(t, "/apis/memory.kubememlens.io/v1alpha1/namespaces/team-a/pods/api", true)
	live := make(chan int, 1)
	go func() { response := httptest.NewRecorder(); h.ServeHTTP(response, liveRequest); live <- response.Code }()
	select {
	case status := <-live:
		if status != http.StatusOK {
			close(release)
			t.Fatalf("live status: %d", status)
		}
	case <-time.After(time.Second):
		close(release)
		t.Fatal("history occupied the live-read gate")
	}
	close(release)
	if response := <-finished; response.Code != http.StatusOK {
		t.Fatalf("history status: %d", response.Code)
	}
}

func TestHistoryFallbackRequiresAnExplicitLocalSelection(t *testing.T) {
	h, _ := historyReadFixture(t)
	localCalls := 0
	h.memoryHistory.remote = historyProviderFunc(func(context.Context, memoryhistory.Selection, memoryhistory.Query) (memoryhistory.Report, error) {
		return memoryhistory.Report{}, memoryhistory.ErrUnavailable
	})
	h.memoryHistory.local = historyProviderFunc(func(_ context.Context, s memoryhistory.Selection, q memoryhistory.Query) (memoryhistory.Report, error) {
		localCalls++
		return memoryhistory.NewReport(s, q, h.now())
	})
	path := "/apis/memory.kubememlens.io/v1alpha1/namespaces/team-a/pods/api/trends"
	if response := serveRead(t, h, path+"?source=prometheus"); response.Code != http.StatusServiceUnavailable || localCalls != 0 {
		t.Fatal("provider failure silently changed source")
	}
	response := serveRead(t, h, path+"?source=local")
	var body memoryHistoryResource
	decodeRead(t, response, &body)
	if response.Code != http.StatusOK || localCalls != 1 || body.History.Query.Metric != memoryhistory.Charge || body.History.Query.Source != memoryhistory.Local {
		t.Fatal("explicit fallback lost local semantics")
	}
}
