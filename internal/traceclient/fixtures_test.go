package traceclient

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	admission "github.com/danushkastanley/kube-memlens/internal/traceadmission"
	"github.com/danushkastanley/kube-memlens/internal/tracecompat"
	"github.com/danushkastanley/kube-memlens/internal/tracepreflight"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
)

func fixtureClient(t *testing.T, handler http.Handler) (*Client, *httptest.Server) {
	t.Helper()
	return unversionedFixtureClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(tracecompat.Header) == tracecompat.Offer {
			w.Header().Set(tracecompat.Header, tracecompat.Selected)
		}
		handler.ServeHTTP(w, r)
	}))
}
func unversionedFixtureClient(t *testing.T, handler http.Handler) (*Client, *httptest.Server) {
	t.Helper()
	s := httptest.NewUnstartedServer(handler)
	s.EnableHTTP2 = true
	s.StartTLS()
	ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: s.Certificate().Raw})
	c, err := New(&rest.Config{Host: s.URL, BearerToken: "fixture-token", TLSClientConfig: rest.TLSClientConfig{CAData: ca}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close(); s.Close() })
	return c, s
}
func selectionFixture() Selection {
	return Selection{Namespace: "tenant-a", Pod: "selected-pod", PodUID: "selected-uid", Container: "worker", ContainerID: strings.Repeat("a", 64), NodeName: "selected-node", ContainerStartedAt: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)}
}
func preflightFixture(t *testing.T, selection Selection, intent Intent) admission.PreflightDocument {
	t.Helper()
	data, err := requestData(selection, intent)
	if err != nil {
		t.Fatal(err)
	}
	r, err := admission.DecodeRequest(selection.Namespace, strings.NewReader(string(data)))
	if err != nil {
		t.Fatal(err)
	}
	p := tracepreflight.Baseline()
	report := tracepreflight.Report{SchemaVersion: 1, Scope: p.Scope, ProfileDigest: p.Digest(), TraceApproval: "pending-custom-programme-freeze", CapturedAt: time.Now().Add(-time.Hour), State: tracepreflight.Supported}
	for _, id := range p.Checks {
		report.Checks = append(report.Checks, tracepreflight.Check{ID: id, State: tracepreflight.Supported, Reason: tracepreflight.Available})
	}
	doc, err := admission.NewPreflightDocument(r, admission.NodePreflight{Baseline: report, EngineDigest: p.EngineDigest, ProgrammeDigest: "sha256:" + strings.Repeat("b", 64), StreamVersion: 2}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return doc
}
func discoverFixture(w http.ResponseWriter) {
	_ = json.NewEncoder(w).Encode(metav1.APIResourceList{TypeMeta: metav1.TypeMeta{Kind: "APIResourceList", APIVersion: "v1"}, GroupVersion: "tracing.kubememlens.io/v1alpha1", APIResources: []metav1.APIResource{
		{Name: "traces", Namespaced: true, Verbs: metav1.Verbs{"create", "get", "delete"}},
		{Name: "traces/stream", Namespaced: true, Verbs: metav1.Verbs{"get"}},
		{Name: "tracepreflights", Namespaced: true, Verbs: metav1.Verbs{"create"}},
	}})
}
func planFixture(t *testing.T, c *Client, doc admission.PreflightDocument) Plan {
	t.Helper()
	return Plan{client: c, selection: selectionFixture(), intent: DefaultIntent(trace.Files), document: doc}
}
func admittedFixture(namespace, id string) admissionDocument {
	d := admissionDocument{APIVersion: "tracing.kubememlens.io/v1alpha1", Kind: "TraceAdmission", State: "admitted", ExpiresAt: time.Now().Add(10 * time.Second), EngineDigest: tracepreflight.Baseline().EngineDigest}
	d.Metadata.Name = id
	d.Metadata.Namespace = namespace
	return d
}
func selectedPlan(t *testing.T, c *Client) Plan {
	t.Helper()
	p, err := c.Preflight(context.Background(), selectionFixture(), DefaultIntent(trace.Files))
	if err != nil {
		t.Fatal(err)
	}
	return p
}
