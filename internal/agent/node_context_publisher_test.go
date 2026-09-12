package agent

import (
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/nodecontext"
	"k8s.io/client-go/rest"
)

func TestNodePublisherRefusesLegacyProjectionBeforePosting(t *testing.T) {
	for _, schema := range []int{1, 2} {
		posts := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost {
				posts++
				t.Error("Node observation posted to an older collector")
			}
			writePublisherJSON(t, w, schemaEpoch(schema, "old-epoch"))
		}))
		publisher, err := newSnapshotPublisher(server.Client(), server.URL)
		if err != nil {
			t.Fatal(err)
		}
		err = publisher.Publish(t.Context(), "uid-a", api.AgentSnapshot{SchemaVersion: 3, NodeContext: &nodecontext.Observation{NodeUID: "uid-a"}})
		server.Close()
		if err == nil || posts != 0 {
			t.Fatal("Node data was silently projected away")
		}
	}
}

func TestNodePublisherRejectsInsecureTargetsAndRedirects(t *testing.T) {
	for _, config := range []*rest.Config{nil, {Host: "http://example.test"}, {Host: "https://example.test", TLSClientConfig: rest.TLSClientConfig{Insecure: true}},
		{Host: "https://user@example.test"}, {Host: "https://example.test?token=secret"}} {
		if _, err := NewNodeContextPublisher(config); err == nil {
			t.Fatal("unsafe publisher target accepted")
		}
	}
	redirected := 0
	target := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { redirected++ }))
	defer target.Close()
	source := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer source.Close()
	ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: source.Certificate().Raw})
	publisher, err := NewNodeContextPublisher(&rest.Config{Host: source.URL, TLSClientConfig: rest.TLSClientConfig{CAData: ca}})
	if err != nil {
		t.Fatal(err)
	}
	publisher.retry.maxAttempts = 1
	if err := publisher.Publish(t.Context(), "uid-a", api.AgentSnapshot{}); err == nil {
		t.Fatal("redirect accepted")
	}
	if redirected != 0 {
		t.Fatal("publisher followed redirect")
	}
}
