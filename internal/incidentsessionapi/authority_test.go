package incidentsessionapi

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/incidentsession"
	"github.com/danushkastanley/kube-memlens/internal/kubeprincipal"
	"k8s.io/apiserver/pkg/authentication/user"
	"k8s.io/apiserver/pkg/authorization/authorizer"
	apirequest "k8s.io/apiserver/pkg/endpoints/request"
)

type namespaceFunc func(context.Context, string) (string, error)

func (f namespaceFunc) Lookup(ctx context.Context, name string) (string, error) { return f(ctx, name) }

func caller(name string) *user.DefaultInfo {
	return &user.DefaultInfo{Name: name, UID: name + "-uid", Groups: []string{user.AllAuthenticated, "operators"}, Extra: map[string][]string{"scope": {"incident"}}}
}

func TestAuthorityOperationAttributes(t *testing.T) {
	for _, test := range []struct {
		op        incidentsession.Operation
		verb, sub string
	}{
		{incidentsession.Create, "create", ""}, {incidentsession.Capture, "create", "capture"}, {incidentsession.Compare, "create", "compare"}, {incidentsession.Markers, "create", "markers"}, {incidentsession.Inspect, "get", ""},
		{incidentsession.Append, "create", "entries"}, {incidentsession.CloseSession, "create", "close"},
		{incidentsession.Delete, "delete", ""}, {incidentsession.ExportSanitised, "get", "export"},
		{incidentsession.ExportAuthorised, "get", "export-sensitive"},
		{incidentsession.ReferenceTrace, "create", "trace-references"},
	} {
		t.Run(string(test.op), func(t *testing.T) {
			id := strings.Repeat("a", 32)
			if test.op == incidentsession.Create {
				id = ""
			}
			identity := caller("alice")
			var captured authorizer.Attributes
			a, err := NewAuthority([]string{"team-a"}, namespaceFunc(func(ctx context.Context, name string) (string, error) {
				if captured == nil || name != "team-a" {
					t.Error("namespace lookup preceded authorisation")
				}
				if _, ok := ctx.Deadline(); !ok {
					t.Error("unbounded lookup")
				}
				return "namespace-uid", nil
			}), authorizer.AuthorizerFunc(func(ctx context.Context, attrs authorizer.Attributes) (authorizer.Decision, string, error) {
				captured = attrs
				return authorizer.DecisionAllow, "", nil
			}))
			if err != nil {
				t.Fatal(err)
			}
			p, err := a.Resolve(apirequest.WithUser(context.Background(), identity), "team-a", test.op, id)
			if err != nil {
				t.Fatal(err)
			}
			if captured.GetVerb() != test.verb || captured.GetSubresource() != test.sub || captured.GetName() != id || captured.GetNamespace() != "team-a" || captured.GetResource() != Resource || captured.GetAPIGroup() != api.MemoryAPIGroup || captured.GetAPIVersion() != api.MemoryAPIVersion || !captured.IsResourceRequest() {
				t.Fatal("incorrect delegated resource attributes")
			}
			if p.Namespace != "team-a" || p.NamespaceUID != "namespace-uid" || len(p.Actor) != 64 || strings.Contains(p.Actor, "alice") {
				t.Fatal("incorrect derived principal")
			}
			identity.Groups[1] = "changed"
			identity.Extra["scope"][0] = "changed"
			if captured.GetUser().GetGroups()[1] != "operators" || captured.GetUser().GetExtra()["scope"][0] != "incident" {
				t.Fatal("mutable delegated claims retained")
			}
		})
	}
}

func TestAuthorityFailsClosedBeforeNamespaceRead(t *testing.T) {
	for _, test := range []struct {
		name              string
		identity          user.Info
		decision          authorizer.Decision
		delegateErr, want error
	}{
		{"missing identity", nil, authorizer.DecisionAllow, nil, kubeprincipal.ErrUnauthenticated},
		{"anonymous", &user.DefaultInfo{Name: user.Anonymous}, authorizer.DecisionAllow, nil, kubeprincipal.ErrUnauthenticated},
		{"deny", caller("alice"), authorizer.DecisionDeny, nil, incidentsession.ErrDenied},
		{"no opinion", caller("alice"), authorizer.DecisionNoOpinion, nil, incidentsession.ErrDenied},
		{"delegate unavailable", caller("alice"), authorizer.DecisionAllow, errors.New("private detail"), incidentsession.ErrUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			a, err := NewAuthority([]string{"team-a"}, namespaceFunc(func(context.Context, string) (string, error) {
				t.Error("namespace read without authorisation")
				return "", nil
			}), authorizer.AuthorizerFunc(func(context.Context, authorizer.Attributes) (authorizer.Decision, string, error) {
				return test.decision, "private reason", test.delegateErr
			}))
			if err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			if test.identity != nil {
				ctx = apirequest.WithUser(ctx, test.identity)
			}
			_, err = a.Resolve(ctx, "team-a", incidentsession.Create, "")
			if !errors.Is(err, test.want) {
				t.Fatalf("got %v, want %v", err, test.want)
			}
		})
	}
}

func TestAuthorityRechecksIdentityAndNamespaceLifetime(t *testing.T) {
	uid := "original-namespace"
	decision := authorizer.DecisionAllow
	a, err := NewAuthority([]string{"team-a"}, namespaceFunc(func(context.Context, string) (string, error) { return uid, nil }), authorizer.AuthorizerFunc(func(context.Context, authorizer.Attributes) (authorizer.Decision, string, error) {
		return decision, "", nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	identity := caller("alice")
	ctx := apirequest.WithUser(context.Background(), identity)
	p, err := a.Resolve(ctx, "team-a", incidentsession.Create, "")
	if err != nil {
		t.Fatal(err)
	}
	id := strings.Repeat("a", 32)
	identity.Groups[1] = "new-authorised-group"
	if err := a.Authorize(ctx, p, incidentsession.Inspect, id); err != nil {
		t.Fatal("mutable groups changed ownership")
	}
	decision = authorizer.DecisionDeny
	if !errors.Is(a.Authorize(ctx, p, incidentsession.Inspect, id), incidentsession.ErrDenied) {
		t.Fatal("revocation ignored")
	}
	decision = authorizer.DecisionAllow
	uid = "recreated-namespace"
	if !errors.Is(a.Authorize(ctx, p, incidentsession.Inspect, id), incidentsession.ErrDenied) {
		t.Fatal("namespace recreation ignored")
	}
	uid = p.NamespaceUID
	identity.UID = "recreated-actor"
	if !errors.Is(a.Authorize(ctx, p, incidentsession.Inspect, id), incidentsession.ErrDenied) {
		t.Fatal("actor recreation ignored")
	}
}

func TestAuthorityScopeAndConfiguration(t *testing.T) {
	for _, names := range [][]string{nil, {"team-a", "team-a"}, {"../team-a"}, {""}, make([]string, 65)} {
		if ValidateNamespaces(names) == nil {
			t.Fatal("invalid namespace configuration accepted")
		}
	}
	a, err := NewAuthority([]string{"team-a"}, namespaceFunc(func(context.Context, string) (string, error) {
		t.Error("invalid request reached resolver")
		return "", nil
	}), authorizer.AuthorizerFunc(func(context.Context, authorizer.Attributes) (authorizer.Decision, string, error) {
		t.Error("invalid request reached delegate")
		return authorizer.DecisionAllow, "", nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	ctx := apirequest.WithUser(context.Background(), caller("alice"))
	if _, err := a.Resolve(ctx, "team-b", incidentsession.Create, ""); !errors.Is(err, incidentsession.ErrNotFound) {
		t.Fatal("unconfigured namespace accepted")
	}
	for _, test := range []struct {
		op incidentsession.Operation
		id string
	}{{"unknown", ""}, {incidentsession.Create, strings.Repeat("a", 32)}, {incidentsession.Inspect, ""}, {incidentsession.Inspect, strings.Repeat("A", 32)}} {
		if _, err := a.Resolve(ctx, "team-a", test.op, test.id); !errors.Is(err, incidentsession.ErrInvalid) {
			t.Fatal("invalid operation accepted")
		}
	}
}
