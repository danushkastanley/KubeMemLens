package extension

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/collector"
)

func serveNodeRead(t *testing.T, handler *ReadHandler, path, schema string) *httptest.ResponseRecorder {
	t.Helper()
	request := readRequest(t, "/apis/memory.kubememlens.io/v1alpha1"+path, true)
	request.Header.Set(api.SnapshotSchemaHeader, schema)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func TestNodeContextReadsExcludePodDataAndProjectLegacyDebug(t *testing.T) {
	now := time.Now().UTC()
	store := collector.NewStore()
	store.EnableNodeContext()
	if err := store.ReconcileNodeIdentities(map[string]string{"node-a": "node-uid-a"}, now); err != nil {
		t.Fatal(err)
	}
	sample := nodeRequest(now, 1).Snapshot.NodeContext
	if err := store.ReplaceNodeContext(*sample); err != nil {
		t.Fatal(err)
	}
	cgroup := testRequest(now, "epoch-a", 1, "node-a", "node-uid-a").Snapshot
	cgroup.Containers[0].PodName = "private-pod"
	cgroup.Containers[0].Namespace = "private-tenant"
	if _, err := store.ReplaceNodeSnapshot(cgroup); err != nil {
		t.Fatal(err)
	}
	handler := NewReadHandler(store, collector.DefaultHandlerOptions(time.Minute))
	handler.nodeContextEnabled = true
	for _, path := range []string{"/nodecontexts", "/nodecontexts/node-a", "/nodecontexts/node-a/history"} {
		response := serveNodeRead(t, handler, path, "3")
		if response.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", path, response.Code, response.Body.String())
		}
		for _, secret := range []string{"private-pod", "private-tenant", "containerCount", "cgroupPath", "containerID"} {
			if strings.Contains(response.Body.String(), secret) {
				t.Fatalf("Node read leaked %s", secret)
			}
		}
	}
	for _, schema := range []string{"", "1", "2"} {
		if response := serveNodeRead(t, handler, "/nodecontexts", schema); response.Code != http.StatusNotFound {
			t.Fatal("legacy schema received Node contract")
		}
		response := serveNodeRead(t, handler, "/clusterstatus/current", schema)
		if response.Code != http.StatusOK || strings.Contains(response.Body.String(), `"nodeContext"`) {
			t.Fatal("legacy debug gained unknown field")
		}
	}
	response := serveNodeRead(t, handler, "/clusterstatus/current", "3")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"nodeContext"`) {
		t.Fatal("current debug missing Node capacity")
	}
	handler.opts.MaxResponseBytes = 128
	if response := serveNodeRead(t, handler, "/nodecontexts/node-a", "3"); response.Code != http.StatusInsufficientStorage {
		t.Fatal("response ceiling ignored")
	}
	if response := serveNodeRead(t, handler, "/nodecontexts/node-a/history", "3"); response.Code != http.StatusInsufficientStorage {
		t.Fatal("history ceiling ignored")
	}
}

func TestNodeContextReadDisabledAndNamespaceRoutesFailBeforeStore(t *testing.T) {
	handler := NewReadHandler(nil, collector.DefaultHandlerOptions(time.Minute))
	if got := serveNodeRead(t, handler, "/nodecontexts", "3"); got.Code != http.StatusNotFound {
		t.Fatal("disabled source exposed reads")
	}
	handler.nodeContextEnabled = true
	if got := serveNodeRead(t, handler, "/namespaces/team-a/nodecontexts", "3"); got.Code != http.StatusNotFound {
		t.Fatal("namespace route exposed full Node data")
	}
}
