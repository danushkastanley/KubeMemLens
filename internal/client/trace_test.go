package client

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
)

func TestTraceUsesFrozenMemoryEndpointAndCredentials(t *testing.T) {
	requests := make(chan string, 1)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- r.Header.Get("Authorization")
		_ = json.NewEncoder(w).Encode(metav1.APIResourceList{TypeMeta: metav1.TypeMeta{Kind: "APIResourceList", APIVersion: "v1"}, GroupVersion: "tracing.kubememlens.io/v1alpha1", APIResources: []metav1.APIResource{
			{Name: "traces", Namespaced: true, Verbs: metav1.Verbs{"create", "get", "delete"}}, {Name: "traces/stream", Namespaced: true, Verbs: metav1.Verbs{"get"}}, {Name: "tracepreflights", Namespaced: true, Verbs: metav1.Verbs{"create"}},
		}})
	}))
	defer server.Close()
	ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	config := &rest.Config{Host: server.URL, BearerToken: "original-identity", TLSClientConfig: rest.TLSClientConfig{CAData: ca}}
	reader, err := NewKubernetesAPIClient(config, AllNamespacesScope(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	config.Host = "https://different-context.invalid"
	config.BearerToken = "different-identity"
	for i := range ca {
		ca[i] = 'x'
	}
	t.Setenv("KUBECONFIG", t.TempDir()+"/unrelated")
	trace, err := reader.NewTraceClient()
	if err != nil {
		t.Fatal(err)
	}
	defer trace.Close()
	if err := trace.Discover(context.Background()); err != nil {
		t.Fatal("trace reread changed context or trust", err)
	}
	if value := <-requests; value != "Bearer original-identity" {
		t.Fatal("identity drifted")
	}
}
