package admissionapi

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	admission "github.com/danushkastanley/kube-memlens/internal/traceadmission"
	"k8s.io/apiserver/pkg/authentication/user"
	"k8s.io/apiserver/pkg/endpoints/request"
)

type denied struct{}

func (denied) Trace(context.Context, user.Info, admission.Operation, string, string) error {
	return admission.ErrDenied
}
func (denied) Pod(context.Context, user.Info, string, string) error { return admission.ErrDenied }

type noTarget struct{ t *testing.T }

func (n noTarget) Resolve(context.Context, admission.Request) (admission.Workload, error) {
	n.t.Error("target work after denial")
	return admission.Workload{}, admission.ErrUnavailable
}
func (n noTarget) Revalidate(context.Context, admission.Workload) error {
	n.t.Error("target work after denial")
	return admission.ErrUnavailable
}
func (n noTarget) Bind(context.Context, string, admission.Workload, time.Time) (admission.Binding, error) {
	n.t.Error("node work after denial")
	return nil, admission.ErrUnavailable
}
func TestHandlerRejectsDirectHeadersAndRetargeting(t *testing.T) {
	manager, err := admission.NewManager(context.Background(), admission.Dependencies{Authorizer: denied{}, Resolver: noTarget{t}, Binder: noTarget{t}, Audit: func(admission.AuditEvent) {}}, admission.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close(context.Background())
	for _, tc := range []struct {
		name, body, path string
		principal        bool
		status           int
	}{
		{"forged headers", `{"schemaVersion":1,"pod":"target","container":"worker","kind":"files"}`, prefix + "/namespaces/tenant-a/traces", false, 401},
		{"denied target", `{"schemaVersion":1,"pod":"target","container":"worker","kind":"files"}`, prefix + "/namespaces/tenant-a/traces", true, 403},
		{"raw node", `{"schemaVersion":1,"pod":"target","container":"worker","kind":"files","node":"other"}`, prefix + "/namespaces/tenant-a/traces", true, 400},
		{"query retarget", `{"schemaVersion":1,"pod":"target","container":"worker","kind":"files"}`, prefix + "/namespaces/tenant-a/traces?namespace=tenant-b", true, 400},
		{"no namespace", `{}`, prefix + "/traces", true, 404},
		{"unknown path", `{}`, prefix + "/namespaces/tenant-a/traces/extra/path", true, 404},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", tc.path, strings.NewReader(tc.body))
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("X-Remote-User", "administrator")
			r.Header.Set("Authorization", "Bearer forged")
			if tc.principal {
				r = r.WithContext(request.WithUser(r.Context(), &user.DefaultInfo{Name: "tenant", Groups: []string{"system:authenticated"}}))
			}
			response := httptest.NewRecorder()
			NewHandler(manager).ServeHTTP(response, r)
			if response.Code != tc.status {
				t.Fatalf("status %d, want %d", response.Code, tc.status)
			}
			for _, private := range []string{"tenant-a", "target", "worker", "administrator", "forged"} {
				if strings.Contains(response.Body.String(), private) {
					t.Fatal("failure disclosed request content")
				}
			}
			if response.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("missing cache boundary")
			}
		})
	}
}
