package extension

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/incidentsession"
	"github.com/danushkastanley/kube-memlens/internal/kube"
	"k8s.io/apiserver/pkg/authorization/authorizer"
)

type podIdentityFunc func(context.Context, string, string) (kube.SessionPodIdentity, error)

func (f podIdentityFunc) LookupPod(ctx context.Context, ns, name string) (kube.SessionPodIdentity, error) {
	return f(ctx, ns, name)
}

func TestIncidentCaptureRevalidatesSourceAndPermissions(t *testing.T) {
	for _, test := range []struct {
		name     string
		changeAt int
		denyAt   int
		want     error
	}{
		{name: "same Pod"},
		{name: "stale collector identity", changeAt: 1, want: incidentsession.ErrChanged},
		{name: "replacement during acquisition", changeAt: 2, want: incidentsession.ErrChanged},
		{name: "initial denial", denyAt: 1, want: incidentsession.ErrDenied},
		{name: "revoked memory permission", denyAt: 3, want: incidentsession.ErrDenied},
		{name: "revoked Pod permission", denyAt: 4, want: incidentsession.ErrDenied},
	} {
		t.Run(test.name, func(t *testing.T) {
			reads, _ := populatedReadHandler(t)
			authCalls, identityCalls := 0, 0
			reads.podAuthorizer = authorizer.AuthorizerFunc(func(_ context.Context, a authorizer.Attributes) (authorizer.Decision, string, error) {
				authCalls++
				group := api.MemoryAPIGroup
				if authCalls%2 == 0 {
					group = ""
				}
				if a.GetAPIGroup() != group || a.GetResource() != "pods" || a.GetName() != "api" || a.GetNamespace() != "team-a" || a.GetVerb() != "get" {
					t.Error("capture authorised wrong object")
				}
				if authCalls == test.denyAt {
					return authorizer.DecisionDeny, "", nil
				}
				return authorizer.DecisionAllow, "", nil
			})
			source := sessionCaptureSource{reads: reads, identity: podIdentityFunc(func(_ context.Context, ns, name string) (kube.SessionPodIdentity, error) {
				identityCalls++
				if ns != "team-a" || name != "api" {
					t.Error("identity read crossed scope")
				}
				uid := "uid-a"
				if identityCalls == test.changeAt {
					uid = "new-pod"
				}
				return kube.SessionPodIdentity{UID: uid, NodeName: "node-a"}, nil
			})}
			ctx := readRequest(t, "/apis/"+readAPIVersion+"/namespaces/team-a/pods/api", true).Context()
			data, err := source.CapturePod(ctx, incidentsession.Principal{Namespace: "team-a", NamespaceUID: "namespace-uid", Actor: "operator"}, "api")
			if !errors.Is(err, test.want) {
				t.Fatalf("got %v want %v", err, test.want)
			}
			if err != nil && len(data) != 0 {
				t.Fatal("failed acquisition returned partial evidence")
			}
			if test.denyAt == 1 && identityCalls != 0 {
				t.Fatal("privileged read preceded permission")
			}
			if err == nil {
				var bundle api.IncidentBundle
				if json.Unmarshal(data, &bundle) != nil || len(bundle.Pods) != 1 || bundle.Pods[0].PodUID != "uid-a" || len(bundle.Nodes) != 0 || !bundle.Partial || authCalls != 4 || identityCalls != 2 {
					t.Fatal("incomplete capture provenance or isolation")
				}
			}
		})
	}
}
