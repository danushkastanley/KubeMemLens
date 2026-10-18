package extension

import (
	"context"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/kube"
	"k8s.io/apiserver/pkg/authentication/user"
	"k8s.io/apiserver/pkg/authorization/authorizer"
	apirequest "k8s.io/apiserver/pkg/endpoints/request"
)

func TestHistoryAuthorisationKeepsNamedSubresourceAndOriginalCaller(t *testing.T) {
	principal := &user.DefaultInfo{Name: "fixture-reader", Groups: []string{"fixture-group"}}
	called := false
	h := &ReadHandler{podAuthorizer: authorizer.AuthorizerFunc(func(_ context.Context, a authorizer.Attributes) (authorizer.Decision, string, error) {
		called = true
		if a.GetUser() != principal || a.GetAPIGroup() != api.MemoryAPIGroup || a.GetAPIVersion() != api.MemoryAPIVersion || a.GetResource() != "nodes" || a.GetSubresource() != "trends" || a.GetName() != "node-a" || a.GetNamespace() != "" || a.GetVerb() != "get" || !a.IsResourceRequest() {
			t.Fatal("named history authorisation broadened its target or caller")
		}
		return authorizer.DecisionAllow, "", nil
	})}
	ctx := apirequest.WithUser(t.Context(), principal)
	if err := h.authoriseVolumeObject(ctx, kube.ObjectAccess{Group: api.MemoryAPIGroup, Resource: "nodes", Subresource: "trends", Name: "node-a"}); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("history bypassed delegated authorisation")
	}
}
