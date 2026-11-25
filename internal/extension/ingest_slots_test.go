package extension

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/collector"
)

const snapshotPath = "/apis/memory.kubememlens.io/v1alpha1/nodesnapshots"

// slotServer serves the ingestion handler over real connections, so that body
// read deadlines behave as they do in the collector. The X-Test-Node header
// stands in for the authenticated producer.
func slotServer(t *testing.T, now time.Time, bodyTimeout time.Duration) *httptest.Server {
	t.Helper()
	handler, err := NewHandler(testCoordinator(t, collector.NewStore(), now, 10), HandlerOptions{
		AgentUsername:    "system:serviceaccount:kube-memlens:kube-memlens-agent",
		MaxSnapshotBytes: 1 << 20, MaxConcurrent: 2, RequestsPerSec: 100, Burst: 100, MaxIdentities: 20,
		BodyReadTimeout: bodyTimeout,
	})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	mux := newTestRouteMux()
	handler.Register(mux)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		node := r.Header.Get("X-Test-Node")
		mux.ServeHTTP(w, withClaims(r, testClaims("pod-"+node, node, "uid-"+node)))
	}))
	t.Cleanup(server.Close)
	return server
}

// stalledPost sends the headers of a snapshot post and then no body, which is
// what a producer holding a slot looks like to the collector.
func stalledPost(t *testing.T, server *httptest.Server, node string) net.Conn {
	t.Helper()
	conn, err := net.Dial("tcp", server.Listener.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	_, err = fmt.Fprintf(conn, "POST %s HTTP/1.1\r\nHost: collector\r\nContent-Type: application/json\r\nX-Test-Node: %s\r\nContent-Length: 4096\r\n\r\n{", snapshotPath, node)
	if err != nil {
		t.Fatalf("write stalled request: %v", err)
	}
	return conn
}

func postSnapshot(t *testing.T, server *httptest.Server, now time.Time, node string) (int, string) {
	t.Helper()
	body, err := json.Marshal(testRequest(now, "epoch-a", 1, node, "uid-"+node))
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, server.URL+snapshotPath, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Test-Node", node)
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatalf("post snapshot: %v", err)
	}
	defer response.Body.Close()
	text, _ := io.ReadAll(response.Body)
	return response.StatusCode, string(text)
}

// One producer credential must not be able to hold every ingestion slot. With
// two slots, a producer that stalls as many posts as it likes still leaves
// room for another Node.
func TestOneProducerCannotHoldEveryIngestionSlot(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	server := slotServer(t, now, time.Minute)
	for range 4 {
		stalledPost(t, server, "node-slow")
	}
	time.Sleep(200 * time.Millisecond) // let the stalled posts reach the handler

	status, body := postSnapshot(t, server, now, "node-a")
	if status != http.StatusOK {
		t.Fatalf("another Node's snapshot while one producer stalls four posts: status=%d body=%s, want 200", status, body)
	}
}

// A body that does not arrive must give its slot back at the read deadline,
// and the stalled producer is told why.
func TestStalledSnapshotBodyIsCutAtTheReadDeadline(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	server := slotServer(t, now, 300*time.Millisecond)
	conn := stalledPost(t, server, "node-slow")

	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	reply := make([]byte, 512)
	n, err := conn.Read(reply)
	if err != nil {
		t.Fatalf("no response to a stalled body within 5s: %v", err)
	}
	if !strings.HasPrefix(string(reply[:n]), "HTTP/1.1 408") {
		t.Fatalf("stalled body: response %q, want 408", string(reply[:n]))
	}
	if status, body := postSnapshot(t, server, now, "node-slow"); status != http.StatusOK {
		t.Fatalf("the producer's next snapshot after its stalled one was cut: status=%d body=%s, want 200", status, body)
	}
}
