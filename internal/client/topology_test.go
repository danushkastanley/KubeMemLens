package client

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/memorytopology"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
)

func TestTopologyTransportBoundsAndScope(t *testing.T) {
	now := time.Now().UTC()
	report, err := memorytopology.Analyse(memorytopology.Failure("node-a", "uid-a", now), now)
	if err != nil {
		t.Fatal(err)
	}
	response := api.NodeMemoryTopology{TypeMeta: metav1.TypeMeta{APIVersion: api.MemoryAPIGroup + "/" + api.MemoryAPIVersion, Kind: "NodeMemoryTopology"}, ObjectMeta: metav1.ObjectMeta{Name: "node-a"}, Current: &report}
	status := 200
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/apis/memory.kubememlens.io/v1alpha1/nodecontexts/node-a/topology" || r.Header.Get(api.SnapshotSchemaHeader) != "7" || r.URL.RawQuery != "" {
			t.Error("scope changed")
		}
		if status != 200 {
			w.WriteHeader(status)
			return
		}
		writeTestJSON(t, w, response)
	}))
	defer server.Close()
	c, err := NewKubernetesAPIClient(&rest.Config{Host: server.URL}, AllNamespacesScope(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.NodeTopology(t.Context(), "node-a"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.NodeTopology(t.Context(), "../node-a"); err == nil || calls != 1 {
		t.Fatal("invalid Node requested")
	}
	response.Current.Observation.NodeName = "other"
	if _, err := c.NodeTopology(t.Context(), "node-a"); err == nil {
		t.Fatal("mismatched Node response accepted")
	}
	status = 403
	if result, err := c.NodeTopology(t.Context(), "node-a"); !IsForbidden(err) || result.Current != nil {
		t.Fatal("revoked permission reused data")
	}
}
