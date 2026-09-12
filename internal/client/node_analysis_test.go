package client

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/nodeanalysis"
	"k8s.io/client-go/rest"
)

func TestNodeAnalysisTransportAndRevocation(t *testing.T) {
	calls := 0
	status := http.StatusOK
	result := nodeanalysis.Analysis{SchemaVersion: nodeanalysis.SchemaVersion, ContributorAccess: nodeanalysis.ClusterPods,
		Rankings: &nodeanalysis.Rankings{Pods: []nodeanalysis.Contributor{{Name: "private-pod"}}}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != http.MethodGet || r.URL.Path != "/apis/memory.kubememlens.io/v1alpha1/nodecontexts/node-a/analysis" ||
			r.Header.Get(api.SnapshotSchemaHeader) != strconv.Itoa(api.CurrentSnapshotSchemaVersion) || r.URL.Query().Get("rank") != "psi" || r.URL.Query().Get("limit") != "20" {
			t.Error("analysis request changed its negotiated resource or bounds")
		}
		if status != http.StatusOK {
			w.WriteHeader(status)
			return
		}
		writeTestJSON(t, w, api.NodeMemoryAnalysis{Analysis: result})
	}))
	defer server.Close()
	reader, err := NewKubernetesAPIClient(&rest.Config{Host: server.URL}, AllNamespacesScope(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	first, err := reader.NodeAnalysis(t.Context(), "node-a", nodeanalysis.PSI, 0)
	if err != nil || first.Rankings == nil {
		t.Fatal("authorised analysis unavailable", err)
	}
	result = nodeanalysis.Analysis{SchemaVersion: nodeanalysis.SchemaVersion, ContributorAccess: nodeanalysis.NodeOnly}
	next, err := reader.NodeAnalysis(t.Context(), "node-a", nodeanalysis.PSI, 0)
	if err != nil || next.Rankings != nil || next.ContributorAccess != nodeanalysis.NodeOnly {
		t.Fatal("revocation retained contributors", err)
	}
	for _, limit := range []int{-1, 101} {
		if _, err := reader.NodeAnalysis(t.Context(), "node-a", nodeanalysis.PSI, limit); err == nil {
			t.Fatal("invalid limit accepted")
		}
	}
	if _, err := reader.NodeAnalysis(t.Context(), "../node-a", nodeanalysis.PSI, 0); err == nil || calls != 2 {
		t.Fatal("invalid input reached transport")
	}
	result.SchemaVersion++
	if _, err := reader.NodeAnalysis(t.Context(), "node-a", nodeanalysis.PSI, 0); err == nil {
		t.Fatal("future analysis schema accepted")
	}
	status = http.StatusForbidden
	_, err = reader.NodeAnalysis(t.Context(), "node-a", nodeanalysis.PSI, 0)
	var failure *ReadError
	if !errors.As(err, &failure) || failure.Kind != ReadErrorForbidden {
		t.Fatal("denied analysis did not preserve the transport error", err)
	}
}
