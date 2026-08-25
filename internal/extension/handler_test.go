package extension

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/collector"
	"k8s.io/apiserver/pkg/authentication/user"
	apirequest "k8s.io/apiserver/pkg/endpoints/request"
)

func TestHandlerRequiresAgentIdentityForEpochAndSnapshot(t *testing.T) {
	handler, _, _ := testHandler(t, 10, 10, 1)
	for _, path := range []string{
		"/apis/memory.kubememlens.io/v1alpha1/ingestionepochs/current",
		"/apis/memory.kubememlens.io/v1alpha1/nodesnapshots",
	} {
		method := http.MethodGet
		if strings.HasSuffix(path, "nodesnapshots") {
			method = http.MethodPost
		}
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(method, path, nil))
		if recorder.Code != http.StatusForbidden {
			t.Fatalf("%s status = %d, want 403", path, recorder.Code)
		}
	}
}

func TestHandlerAcceptsValidSnapshotAndReturnsDuplicate(t *testing.T) {
	handler, _, now := testHandler(t, 1<<20, 10, 2)
	request := testRequest(now, "epoch-a", 1, "node-a", "node-uid-a")
	first := serveSnapshot(t, handler, testClaims("pod-a", "node-a", "node-uid-a"), request, "")
	if first.Code != http.StatusOK {
		t.Fatalf("first status = %d: %s", first.Code, first.Body.String())
	}
	second := serveSnapshot(t, handler, testClaims("pod-a", "node-a", "node-uid-a"), request, "")
	if second.Code != http.StatusOK {
		t.Fatalf("duplicate status = %d: %s", second.Code, second.Body.String())
	}
	var response api.NodeSnapshotResponse
	if err := json.Unmarshal(second.Body.Bytes(), &response); err != nil || !response.Duplicate {
		t.Fatalf("duplicate response = %#v, err=%v", response, err)
	}
}

func TestHandlerRejectsCompressedAndOversizedBodies(t *testing.T) {
	handler, _, now := testHandler(t, 1, 10, 1)
	claims := testClaims("pod-a", "node-a", "node-uid-a")
	compressed := serveSnapshot(t, handler, claims, testRequest(now, "epoch-a", 1, "node-a", "node-uid-a"), "gzip")
	if compressed.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("compressed status = %d, want 415", compressed.Code)
	}

	body := bytes.Repeat([]byte("x"), 2)
	request := httptest.NewRequest(http.MethodPost, "/apis/memory.kubememlens.io/v1alpha1/nodesnapshots", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request = withClaims(request, claims)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized status = %d, want 413: %s", recorder.Code, recorder.Body.String())
	}
}

func TestHandlerRateLimitIsPerNodeAcrossAgentRotation(t *testing.T) {
	handler, _, now := testHandler(t, 1<<20, 0.0001, 1)
	firstClaims := testClaims("pod-a", "node-a", "node-uid-a")
	first := serveSnapshot(t, handler, firstClaims, testRequest(now, "epoch-a", 1, "node-a", "node-uid-a"), "")
	if first.Code != http.StatusOK {
		t.Fatalf("first status = %d", first.Code)
	}
	limited := serveSnapshot(t, handler, firstClaims, testRequest(now.Add(time.Second), "epoch-a", 2, "node-a", "node-uid-a"), "")
	if limited.Code != http.StatusTooManyRequests {
		t.Fatalf("same identity status = %d, want 429", limited.Code)
	}
	replacementClaims := testClaims("pod-replacement", "node-a", "node-uid-a")
	replacement := serveSnapshot(t, handler, replacementClaims, testRequest(now.Add(time.Second), "epoch-a", 1, "node-a", "node-uid-a"), "")
	if replacement.Code != http.StatusTooManyRequests {
		t.Fatalf("replacement on same node status = %d, want 429", replacement.Code)
	}
	otherClaims := testClaims("pod-b", "node-b", "node-uid-b")
	other := serveSnapshot(t, handler, otherClaims, testRequest(now, "epoch-a", 1, "node-b", "node-uid-b"), "")
	if other.Code != http.StatusOK {
		t.Fatalf("other identity status = %d, want 200: %s", other.Code, other.Body.String())
	}
}

func TestHandlerLimiterCapacityEvictsLeastRecentlySeenNode(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	coordinator := testCoordinator(t, collector.NewStore(), now, 2)
	handler, err := NewHandler(coordinator, HandlerOptions{
		AgentUsername:    "system:serviceaccount:kube-memlens:kube-memlens-agent",
		MaxSnapshotBytes: 1 << 20, MaxConcurrent: 1, RequestsPerSec: 1, Burst: 1, MaxIdentities: 2,
		IdentityTTL: time.Hour,
	})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	if !handler.allowIdentity("node-a", now) || !handler.allowIdentity("node-b", now.Add(time.Second)) ||
		!handler.allowIdentity("node-c", now.Add(2*time.Second)) {
		t.Fatal("new node was rejected at limiter capacity")
	}
	if len(handler.limiters) != 2 {
		t.Fatalf("limiter count = %d, want 2", len(handler.limiters))
	}
	if _, exists := handler.limiters["node-a"]; exists {
		t.Fatal("least recently seen limiter was not evicted")
	}
}

func TestHandlerLogsAndMetricsUseBoundedResults(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	store := collector.NewStore()
	coordinator := testCoordinator(t, store, now, 10)
	logs := []string{}
	ingestionHandler, err := NewHandler(coordinator, HandlerOptions{
		AgentUsername:    "system:serviceaccount:kube-memlens:kube-memlens-agent",
		MaxSnapshotBytes: 1 << 20, MaxConcurrent: 2, RequestsPerSec: 10, Burst: 1, MaxIdentities: 20,
		Logf: func(format string, args ...any) { logs = append(logs, fmt.Sprintf(format, args...)) },
	})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	mux := http.NewServeMux()
	ingestionHandler.Register(mux)
	secret := "credential-secret-sentinel"
	claims := testClaims("pod-a", "node-a", "node-uid-a")
	claims.CredentialID = secret
	recorder := serveSnapshot(t, mux, claims, testRequest(now, "wrong", 1, "node-a", "node-uid-a"), "")
	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", recorder.Code)
	}
	stats := store.IngestionStats()
	if len(stats.Results) != 1 || stats.Results["epoch_mismatch"] != 1 {
		t.Fatalf("unexpected ingestion results: %#v", stats.Results)
	}
	if len(logs) != 1 || strings.Contains(logs[0], secret) || !strings.Contains(logs[0], "reason=epoch_mismatch") || !strings.Contains(logs[0], "principal=agent") {
		t.Fatalf("unexpected security log: %#v", logs)
	}
}

func TestHandlerConcurrencyLimitRejectsBeforeReadingSecondBody(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	store := collector.NewStore()
	coordinator := testCoordinator(t, store, now, 10)
	handler, err := NewHandler(coordinator, HandlerOptions{
		AgentUsername:    "system:serviceaccount:kube-memlens:kube-memlens-agent",
		MaxSnapshotBytes: 1 << 20, MaxConcurrent: 1, RequestsPerSec: 10, Burst: 2, MaxIdentities: 20,
	})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	mux := http.NewServeMux()
	handler.Register(mux)
	body, _ := json.Marshal(testRequest(now, "epoch-a", 1, "node-a", "node-uid-a"))
	blocked := &blockingReader{reader: bytes.NewReader(body), started: make(chan struct{}), release: make(chan struct{})}
	firstRequest := httptest.NewRequest(http.MethodPost, "/apis/memory.kubememlens.io/v1alpha1/nodesnapshots", blocked)
	firstRequest.Header.Set("Content-Type", "application/json")
	firstRequest = withClaims(firstRequest, testClaims("pod-a", "node-a", "node-uid-a"))
	firstDone := make(chan struct{})
	go func() {
		defer close(firstDone)
		mux.ServeHTTP(httptest.NewRecorder(), firstRequest)
	}()
	<-blocked.started

	secondBody, _ := json.Marshal(testRequest(now, "epoch-a", 1, "node-b", "node-uid-b"))
	secondReader := &countingReader{reader: bytes.NewReader(secondBody)}
	secondRequest := httptest.NewRequest(http.MethodPost, "/apis/memory.kubememlens.io/v1alpha1/nodesnapshots", secondReader)
	secondRequest.Header.Set("Content-Type", "application/json")
	secondRequest = withClaims(secondRequest, testClaims("pod-b", "node-b", "node-uid-b"))
	secondRecorder := httptest.NewRecorder()
	mux.ServeHTTP(secondRecorder, secondRequest)
	if secondRecorder.Code != http.StatusTooManyRequests || secondReader.reads != 0 {
		t.Fatalf("second request status=%d reads=%d", secondRecorder.Code, secondReader.reads)
	}
	close(blocked.release)
	<-firstDone
}

func testHandler(t *testing.T, maxBytes int64, requestsPerSecond float64, burst int) (http.Handler, *collector.Store, time.Time) {
	t.Helper()
	now := time.Unix(1_800_000_000, 0).UTC()
	store := collector.NewStore()
	coordinator := testCoordinator(t, store, now, 10)
	handler, err := NewHandler(coordinator, HandlerOptions{
		AgentUsername:    "system:serviceaccount:kube-memlens:kube-memlens-agent",
		MaxSnapshotBytes: maxBytes, MaxConcurrent: 2, RequestsPerSec: requestsPerSecond,
		Burst: burst, MaxIdentities: 20,
	})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	mux := http.NewServeMux()
	handler.Register(mux)
	return mux, store, now
}

func serveSnapshot(t *testing.T, handler http.Handler, claims AgentClaims, request api.NodeSnapshotRequest, encoding string) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	httpRequest := httptest.NewRequest(http.MethodPost, "/apis/memory.kubememlens.io/v1alpha1/nodesnapshots", bytes.NewReader(body))
	httpRequest.Header.Set("Content-Type", "application/json")
	if encoding != "" {
		httpRequest.Header.Set("Content-Encoding", encoding)
	}
	httpRequest = withClaims(httpRequest, claims)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httpRequest)
	return recorder
}

func withClaims(request *http.Request, claims AgentClaims) *http.Request {
	info := &user.DefaultInfo{
		Name: "system:serviceaccount:kube-memlens:kube-memlens-agent",
		Extra: map[string][]string{
			PodUIDExtra: {claims.PodUID}, NodeNameExtra: {claims.NodeName}, NodeUIDExtra: {claims.NodeUID},
			CredentialIDExtra: {claims.CredentialID},
		},
	}
	return request.WithContext(apirequest.WithUser(request.Context(), info))
}

type blockingReader struct {
	reader  *bytes.Reader
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (r *blockingReader) Read(buffer []byte) (int, error) {
	r.once.Do(func() { close(r.started) })
	<-r.release
	return r.reader.Read(buffer)
}

type countingReader struct {
	reader *bytes.Reader
	reads  int
}

func (r *countingReader) Read(buffer []byte) (int, error) {
	r.reads++
	return r.reader.Read(buffer)
}
