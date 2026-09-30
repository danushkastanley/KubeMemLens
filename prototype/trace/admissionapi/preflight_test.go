package admissionapi

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	admission "github.com/danushkastanley/kube-memlens/internal/traceadmission"
	"github.com/danushkastanley/kube-memlens/internal/tracepreflight"
	"k8s.io/apiserver/pkg/authentication/user"
	"k8s.io/apiserver/pkg/authorization/authorizer"
	"k8s.io/apiserver/pkg/endpoints/request"
)

type preflightFixture struct {
	denied           bool
	inspected, bound int
}

func (f *preflightFixture) Trace(context.Context, user.Info, admission.Operation, string, string) error {
	if f.denied {
		return admission.ErrDenied
	}
	return nil
}
func (f *preflightFixture) Pod(context.Context, user.Info, string, string) error {
	if f.denied {
		return admission.ErrDenied
	}
	return nil
}
func (*preflightFixture) Resolve(_ context.Context, r admission.Request) (admission.Workload, error) {
	return admission.Workload{Target: trace.TargetIdentity{Namespace: r.Namespace(), PodName: r.Pod(), PodUID: "selected-uid", ContainerName: r.Container(), ContainerID: strings.Repeat("a", 64), ContainerStartedAt: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), NodeUID: "node-uid"}, NodeName: "selected-node", QoS: "Burstable"}, nil
}
func (*preflightFixture) Revalidate(context.Context, admission.Workload) error { return nil }
func (f *preflightFixture) Bind(context.Context, string, admission.Workload, admission.Request, time.Time) (admission.Binding, error) {
	f.bound++
	return nil, admission.ErrUnavailable
}
func (f *preflightFixture) Preflight(context.Context, admission.Workload, admission.Request) (admission.NodePreflight, error) {
	f.inspected++
	p := tracepreflight.Baseline()
	r := tracepreflight.Report{SchemaVersion: 1, Scope: p.Scope, ProfileDigest: p.Digest(), TraceApproval: "pending-custom-programme-freeze", CapturedAt: time.Now(), State: tracepreflight.Supported}
	for _, id := range p.Checks {
		r.Checks = append(r.Checks, tracepreflight.Check{ID: id, State: tracepreflight.Supported, Reason: tracepreflight.Available})
	}
	return admission.NodePreflight{Baseline: r, EngineDigest: p.EngineDigest, ProgrammeDigest: streamDigest, StreamVersion: 2}, nil
}

func TestPublicPreflightRequiresAuthoritySelectionAndInstalledProgramme(t *testing.T) {
	body := `{"schemaVersion":2,"pod":"private-pod","container":"worker","kind":"files","expectedPodUID":"selected-uid","expectedContainerID":"` + strings.Repeat("a", 64) + `","expectedContainerStartedAt":"2026-09-01T00:00:00Z","expectedNodeName":"selected-node"}`
	for _, tc := range []struct {
		name   string
		status int
	}{{"ready", 200}, {"denied", 403}, {"unauthenticated", 401}, {"replacement", 409}, {"unapproved", 503}, {"legacy", 400}} {
		t.Run(tc.name, func(t *testing.T) {
			f := &preflightFixture{denied: tc.name == "denied"}
			m, err := admission.NewManager(context.Background(), admission.Dependencies{Authorizer: f, Resolver: f, Binder: f, AuditReferences: testAuditReferences(t), Audit: func(context.Context, admission.AuditEvent) error { return nil }}, admission.DefaultPolicy())
			if err != nil {
				t.Fatal(err)
			}
			defer m.Close(context.Background())
			h := NewHandler(m)
			h.stream, err = NewStreamProxyVersion(map[trace.Kind]string{trace.Files: streamDigest}, 2)
			if err != nil {
				t.Fatal(err)
			}
			input := body
			if tc.name == "replacement" {
				input = strings.Replace(input, "selected-uid", "other-uid", 1)
			}
			if tc.name == "legacy" {
				input = `{"schemaVersion":1,"pod":"private-pod","container":"worker","kind":"files"}`
			}
			if tc.name == "unapproved" {
				h.stream = nil
			}
			r := httptest.NewRequest("POST", prefix+"/namespaces/tenant-a/tracepreflights", strings.NewReader(input))
			r.Header.Set("Content-Type", "application/json")
			if tc.name != "unauthenticated" {
				r = r.WithContext(request.WithUser(r.Context(), &user.DefaultInfo{Name: "operator", Groups: []string{user.AllAuthenticated}}))
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.status || f.bound != 0 {
				t.Fatalf("status %d want %d; node bindings %d", w.Code, tc.status, f.bound)
			}
			if w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("preflight became cacheable")
			}
			for _, secret := range []string{"private-pod", "selected-uid", "selected-node", strings.Repeat("a", 64)} {
				if strings.Contains(w.Body.String(), secret) {
					t.Fatal("preflight disclosed target")
				}
			}
			if tc.status == 200 {
				var doc admission.PreflightDocument
				if json.Unmarshal(w.Body.Bytes(), &doc) != nil || doc.SchemaVersion != 1 || doc.RequestSchemaVersion != 2 || doc.ResourceQualified || doc.Node.Validate(trace.Files) != nil {
					t.Fatal("invalid preflight contract")
				}
			} else if tc.name != "unapproved" && f.inspected != 0 {
				t.Fatal("failed prerequisite reached node")
			}
		})
	}
}

func TestPreflightDelegationAllowsOnlyNamespacedCreation(t *testing.T) {
	base := authorizer.AttributesRecord{APIGroup: admission.APIGroup, APIVersion: admission.APIVersion, Namespace: "tenant-a", Resource: "tracepreflights", Verb: "create", ResourceRequest: true}
	if !validTraceResource(base) {
		t.Fatal("preflight create rejected")
	}
	for _, change := range []func(*authorizer.AttributesRecord){
		func(a *authorizer.AttributesRecord) { a.Verb = "get" }, func(a *authorizer.AttributesRecord) { a.Verb = "list" }, func(a *authorizer.AttributesRecord) { a.Namespace = "" }, func(a *authorizer.AttributesRecord) { a.Name = "named" }, func(a *authorizer.AttributesRecord) { a.Subresource = "stream" }, func(a *authorizer.AttributesRecord) { a.APIGroup = "other" },
	} {
		a := base
		change(&a)
		if validTraceResource(a) {
			t.Fatal("preflight vocabulary widened")
		}
	}
}
