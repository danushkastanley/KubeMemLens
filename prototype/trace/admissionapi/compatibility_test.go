package admissionapi

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	admission "github.com/danushkastanley/kube-memlens/internal/traceadmission"
	"github.com/danushkastanley/kube-memlens/internal/tracecompat"
	"k8s.io/apiserver/pkg/authentication/user"
	"k8s.io/apiserver/pkg/endpoints/request"
)

func compatibilityHandler(t *testing.T, streamVersion int) (*Handler, *preflightFixture) {
	t.Helper()
	f := &preflightFixture{}
	m, err := admission.NewManager(context.Background(), admission.Dependencies{Authorizer: f, Resolver: f, Binder: f, AuditReferences: testAuditReferences(t), Audit: func(context.Context, admission.AuditEvent) error { return nil }}, admission.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Close(context.Background()) })
	h := NewHandler(m)
	h.stream, err = NewStreamProxyVersion(map[trace.Kind]string{trace.Files: streamDigest}, streamVersion)
	if err != nil {
		t.Fatal(err)
	}
	return h, f
}
func compatibilityBody(schema int) map[string]any {
	value := map[string]any{"schemaVersion": schema, "pod": "private-pod", "container": "worker", "kind": "files",
		"expectedPodUID": "selected-uid", "expectedContainerID": strings.Repeat("a", 64), "expectedContainerStartedAt": "2026-09-01T00:00:00Z", "expectedNodeName": "selected-node"}
	if schema == 3 {
		value["contractVersion"] = 1
	}
	return value
}
func TestPreflightNegotiatesWithoutWeakeningLegacyOrSelectedLimits(t *testing.T) {
	for _, tc := range []struct {
		name   string
		schema int
		offers []string
		status int
	}{
		{"previous client", 2, nil, 200}, {"current", 3, []string{"1-1"}, 200}, {"compatible future reader", 3, []string{"1-2"}, 200},
		{"unsupported", 3, []string{"2-2"}, 406}, {"duplicate", 3, []string{"1-1", "1-1"}, 400},
		{"missing acknowledgement request", 3, nil, 406}, {"older body under current contract", 2, []string{"1-1"}, 406},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, f := compatibilityHandler(t, 2)
			body, _ := json.Marshal(compatibilityBody(tc.schema))
			r := httptest.NewRequest("POST", prefix+"/namespaces/tenant-a/tracepreflights", strings.NewReader(string(body)))
			r.Header.Set("Content-Type", "application/json")
			// Header.Add canonicalises the actual HTTP header name, including duplicates.
			r.Header.Del(tracecompat.Header)
			for _, v := range tc.offers {
				r.Header.Add(tracecompat.Header, v)
			}
			r = r.WithContext(request.WithUser(r.Context(), &user.DefaultInfo{Name: "operator", Groups: []string{user.AllAuthenticated}}))
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.status || f.bound != 0 {
				t.Fatalf("status %d, want %d; bindings %d", w.Code, tc.status, f.bound)
			}
			if tc.status != 200 {
				if f.inspected != 0 {
					t.Fatal("incompatible request reached node inspection")
				}
				return
			}
			var doc admission.PreflightDocument
			if json.Unmarshal(w.Body.Bytes(), &doc) != nil || doc.RequestSchemaVersion != tc.schema || doc.Bounds.Events != 10000 || doc.ResourceQualified {
				t.Fatal("negotiation altered response semantics")
			}
			if tc.schema == 3 && w.Header().Get(tracecompat.Header) != tracecompat.Selected {
				t.Fatal("current contract not acknowledged")
			}
			if tc.schema == 2 && len(w.Header().Values(tracecompat.Header)) != 0 {
				t.Fatal("legacy response claimed a negotiated contract")
			}
		})
	}
}
func TestIncompatibilityRejectsCreateAndStreamBeforeAnyBinding(t *testing.T) {
	for _, tc := range []struct {
		name, method, suffix, offer string
		format, status              int
	}{
		{"create unknown contract", "POST", "traces", "2-2", 2, 406},
		{"experimental stream contract", "POST", "traces", "1-1", 1, 406},
		{"stream unknown contract", "GET", "traces/" + strings.Repeat("a", 32) + "/stream?contract=1", "2-2", 2, 406},
		{"missing activation marker", "GET", "traces/" + strings.Repeat("a", 32) + "/stream", "1-1", 2, 400},
		{"marker without offer", "GET", "traces/" + strings.Repeat("a", 32) + "/stream?contract=1", "", 2, 400},
		{"extra activation query", "GET", "traces/" + strings.Repeat("a", 32) + "/stream?contract=1&extra=x", "1-1", 2, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, f := compatibilityHandler(t, tc.format)
			var body string
			if tc.method == "POST" {
				data, _ := json.Marshal(compatibilityBody(3))
				body = string(data)
			}
			r := httptest.NewRequest(tc.method, prefix+"/namespaces/tenant-a/"+tc.suffix, strings.NewReader(body))
			r.Header.Set("Content-Type", "application/json")
			if tc.offer != "" {
				r.Header.Set(tracecompat.Header, tc.offer)
			}
			r = r.WithContext(request.WithUser(r.Context(), &user.DefaultInfo{Name: "operator", Groups: []string{user.AllAuthenticated}}))
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.status || f.bound != 0 || f.inspected != 0 {
				t.Fatalf("status=%d bound=%d inspected=%d", w.Code, f.bound, f.inspected)
			}
			for _, private := range []string{"private-pod", "selected-uid", strings.Repeat("a", 64)} {
				if strings.Contains(w.Body.String(), private) {
					t.Fatal("negotiation error disclosed a target")
				}
			}
		})
	}
}
func TestIncompatibleOfferCannotDisableAuthorisedCancellation(t *testing.T) {
	h, f := compatibilityHandler(t, 2)
	r := httptest.NewRequest("DELETE", prefix+"/namespaces/tenant-a/traces/"+strings.Repeat("a", 32), nil)
	r.Header.Add(tracecompat.Header, "999-999")
	r.Header.Add(tracecompat.Header, "invalid")
	r = r.WithContext(request.WithUser(r.Context(), &user.DefaultInfo{Name: "operator", Groups: []string{user.AllAuthenticated}}))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 404 || f.bound != 0 {
		t.Fatalf("cancellation did not reach the existing ownership boundary: %d", w.Code)
	}
}
