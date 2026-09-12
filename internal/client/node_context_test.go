package client

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"k8s.io/client-go/rest"
)

func TestNodeContextClientUsesBoundedClusterResources(t *testing.T) {
	calls := 0
	denied := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get(api.SnapshotSchemaHeader) != strconv.Itoa(api.CurrentSnapshotSchemaVersion) || r.Method != http.MethodGet {
			t.Error("Node read did not negotiate the current schema")
		}
		if denied {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		switch r.URL.Path {
		case "/apis/memory.kubememlens.io/v1alpha1/nodecontexts":
			if r.URL.Query().Get("limit") != "100" || r.URL.Query().Get("continue") != "opaque+token" {
				t.Error("Node pagination changed")
			}
			writeTestJSON(t, w, api.NodeContextList{})
		case "/apis/memory.kubememlens.io/v1alpha1/nodecontexts/node-a":
			writeTestJSON(t, w, api.NodeContextResource{Record: api.NodeContextRecord{NodeName: "node-a"}})
		case "/apis/memory.kubememlens.io/v1alpha1/nodecontexts/node-a/history":
			if r.URL.Query().Get("limit") != "1" {
				t.Error("history did not bound instances")
			}
			writeTestJSON(t, w, api.NodeContextHistory{NodeName: "node-a"})
		default:
			t.Error("unexpected Node resource path")
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	reader, err := NewKubernetesAPIClient(&rest.Config{Host: server.URL}, AllNamespacesScope(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reader.NodeContexts(t.Context(), "opaque+token"); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.NodeContext(t.Context(), "node-a"); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.NodeContextHistory(t.Context(), "node-a", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.NodeContext(t.Context(), "../node-a"); err == nil || calls != 3 {
		t.Fatal("invalid Node name reached transport")
	}
	denied = true
	_, err = reader.NodeContext(t.Context(), "node-a")
	var failure *ReadError
	if !errors.As(err, &failure) || failure.Kind != ReadErrorForbidden || calls != 4 {
		t.Fatal("denied Node read fell back", err)
	}
}
