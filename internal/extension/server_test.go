package extension

import (
	"context"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/api"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apiserver/pkg/authentication/user"
	"k8s.io/apiserver/pkg/authorization/authorizer"
	"k8s.io/apiserver/pkg/authorization/path"
	"k8s.io/apiserver/pkg/authorization/union"
	genericfilters "k8s.io/apiserver/pkg/endpoints/filters"
	apirequest "k8s.io/apiserver/pkg/endpoints/request"
	genericapiserver "k8s.io/apiserver/pkg/server"
	"k8s.io/client-go/kubernetes/fake"
)

func TestRequestHeaderAuthenticationRejectsDirectBearerAndForgedHeaders(t *testing.T) {
	tlsServer := httptest.NewTLSServer(nil)
	defer tlsServer.Close()
	certificate := tlsServer.Certificate()
	ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate.Raw})
	client := fake.NewSimpleClientset(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "extension-apiserver-authentication", Namespace: metav1.NamespaceSystem},
		Data: map[string]string{
			"requestheader-client-ca-file":       string(ca),
			"requestheader-username-headers":     `["X-Remote-User"]`,
			"requestheader-uid-headers":          `[]`,
			"requestheader-group-headers":        `["X-Remote-Group"]`,
			"requestheader-extra-headers-prefix": `["X-Remote-Extra-"]`,
			"requestheader-allowed-names":        `["front-proxy-client"]`,
		},
	})
	scheme := runtime.NewScheme()
	metav1.AddToGroupVersion(scheme, schema.GroupVersion{Version: "v1"})
	config := genericapiserver.NewConfig(serializer.NewCodecFactory(scheme))
	_, err := configureRequestHeaderAuthenticationWithClient(context.Background(), client, config)
	if err != nil {
		t.Fatalf("configure request-header authentication: %v", err)
	}
	request := httptest.NewRequest("GET", "/apis/memory.kubememlens.io/v1alpha1", nil)
	request.Header.Set("Authorization", "Bearer credential-sentinel")
	request.Header.Set("X-Remote-User", "system:serviceaccount:kube-memlens:kube-memlens-agent")
	response, authenticated, err := config.Authentication.Authenticator.AuthenticateRequest(request)
	if err != nil {
		t.Fatalf("AuthenticateRequest returned error: %v", err)
	}
	if authenticated || response != nil {
		t.Fatalf("direct request authenticated: %#v", response)
	}
}

func TestRequiredAllowedNamesFailsClosed(t *testing.T) {
	provider := requiredAllowedNames(func() []string { return nil })
	names := provider()
	if len(names) != 1 || names[0] != "\x00invalid-empty-proxy-cn" {
		t.Fatalf("empty provider returned %#v", names)
	}
}

func TestHealthProbePathsDoNotDelegateSubjectAccessReview(t *testing.T) {
	probeAuthorizer, err := path.NewAuthorizer(delegatedAlwaysAllowPaths())
	if err != nil {
		t.Fatalf("create probe authorizer: %v", err)
	}
	delegated := 0
	delegate := union.New(probeAuthorizer, authorizer.AuthorizerFunc(func(context.Context, authorizer.Attributes) (authorizer.Decision, string, error) {
		delegated++
		return authorizer.DecisionDeny, "unexpected delegation", nil
	}))
	gate := agentIdentityAuthorizer{delegate: delegate}
	for _, probePath := range delegatedAlwaysAllowPaths() {
		decision, _, err := gate.Authorize(context.Background(), authorizer.AttributesRecord{
			Verb: "get", Path: probePath, ResourceRequest: false,
		})
		if err != nil || decision != authorizer.DecisionAllow {
			t.Fatalf("%s decision=%v err=%v", probePath, decision, err)
		}
	}
	if delegated != 0 {
		t.Fatalf("health probes delegated %d SubjectAccessReviews", delegated)
	}
	decision, _, err := gate.Authorize(context.Background(), authorizer.AttributesRecord{
		Verb: "get", Path: "/readyz/extra", ResourceRequest: false,
	})
	if err != nil || decision != authorizer.DecisionDeny || delegated != 1 {
		t.Fatalf("non-health path decision=%v delegated=%d err=%v", decision, delegated, err)
	}
}

func TestAgentIdentityAuthorizerRequiresClaimsBeforeDelegation(t *testing.T) {
	delegated := 0
	gate := agentIdentityAuthorizer{
		expectedUsername: "system:serviceaccount:kube-memlens:kube-memlens-agent",
		delegate: authorizer.AuthorizerFunc(func(context.Context, authorizer.Attributes) (authorizer.Decision, string, error) {
			delegated++
			return authorizer.DecisionAllow, "allowed", nil
		}),
	}
	attributes := authorizer.AttributesRecord{
		ResourceRequest: true, APIGroup: api.MemoryAPIGroup, Resource: "nodesnapshots", Verb: "create",
		User: &user.DefaultInfo{Name: "system:serviceaccount:kube-memlens:other"},
	}
	decision, _, err := gate.Authorize(context.Background(), attributes)
	if err != nil || decision != authorizer.DecisionDeny || delegated != 0 {
		t.Fatalf("invalid identity decision=%v delegated=%d err=%v", decision, delegated, err)
	}
	attributes.User = &user.DefaultInfo{
		Name: "system:serviceaccount:kube-memlens:kube-memlens-agent",
		Extra: map[string][]string{
			PodUIDExtra: {"pod-a"}, NodeNameExtra: {"node-a"}, NodeUIDExtra: {"node-uid-a"}, CredentialIDExtra: {"credential-a"},
		},
	}
	decision, _, err = gate.Authorize(context.Background(), attributes)
	if err != nil || decision != authorizer.DecisionAllow || delegated != 1 {
		t.Fatalf("valid identity decision=%v delegated=%d err=%v", decision, delegated, err)
	}
}

func TestTenantReadAuthorizerDelegatesExactAttributesOnceAndAuditsBoundedFields(t *testing.T) {
	delegated := 0
	var received authorizer.Attributes
	logs := []string{}
	gate := agentIdentityAuthorizer{
		expectedUsername: "system:serviceaccount:kube-memlens:kube-memlens-agent",
		delegate: authorizer.AuthorizerFunc(func(_ context.Context, attributes authorizer.Attributes) (authorizer.Decision, string, error) {
			delegated++
			received = attributes
			return authorizer.DecisionAllow, "sensitive delegate reason", nil
		}),
		logf: func(format string, args ...any) { logs = append(logs, fmt.Sprintf(format, args...)) },
	}
	secret := "tenant-secret-sentinel"
	attributes := authorizer.AttributesRecord{
		ResourceRequest: true, APIGroup: api.MemoryAPIGroup, APIVersion: api.MemoryAPIVersion,
		Resource: "pods", Subresource: "history", Verb: "get", Namespace: secret, Name: "pod-" + secret,
		User: &user.DefaultInfo{Name: "user-" + secret, Groups: []string{"group-" + secret}},
	}

	decision, _, err := gate.Authorize(context.Background(), attributes)
	if err != nil || decision != authorizer.DecisionAllow || delegated != 1 {
		t.Fatalf("decision=%v delegated=%d err=%v", decision, delegated, err)
	}
	if received.GetVerb() != "get" || received.GetAPIGroup() != api.MemoryAPIGroup ||
		received.GetAPIVersion() != api.MemoryAPIVersion || received.GetResource() != "pods" ||
		received.GetSubresource() != "history" || received.GetNamespace() != secret || received.GetName() != "pod-"+secret {
		t.Fatalf("delegated attributes changed: %#v", received)
	}
	if len(logs) != 1 || strings.Contains(logs[0], secret) ||
		!strings.Contains(logs[0], "principal=user verb=get resource=pods scope=namespace decision=allow reason=sar_allowed") {
		t.Fatalf("unexpected audit log: %#v", logs)
	}
}

func TestTenantReadAuthorizerPreservesNoOpinionWithoutCachingOrStoreWork(t *testing.T) {
	delegated := 0
	gate := agentIdentityAuthorizer{
		delegate: authorizer.AuthorizerFunc(func(context.Context, authorizer.Attributes) (authorizer.Decision, string, error) {
			delegated++
			return authorizer.DecisionNoOpinion, "", nil
		}),
	}
	attributes := authorizer.AttributesRecord{
		ResourceRequest: true, APIGroup: api.MemoryAPIGroup, APIVersion: api.MemoryAPIVersion,
		Resource: "containers", Verb: "list", Namespace: "team-a", User: &user.DefaultInfo{Name: "reader"},
	}
	for range 2 {
		decision, _, err := gate.Authorize(context.Background(), attributes)
		if err != nil || decision != authorizer.DecisionNoOpinion {
			t.Fatalf("decision=%v err=%v", decision, err)
		}
	}
	if delegated != 2 {
		t.Fatalf("delegated=%d, want one exact decision per request", delegated)
	}
}

func TestResourceRoutesDelegateExactAuthorisationAttributes(t *testing.T) {
	tests := []struct {
		name, method, path                           string
		verb, resource, subresource, namespace, item string
		agent                                        bool
	}{
		{name: "namespaced Pod list", method: http.MethodGet, path: "/apis/memory.kubememlens.io/v1alpha1/namespaces/team-a/pods", verb: "list", resource: "pods", namespace: "team-a"},
		{name: "namespaced Pod get", method: http.MethodGet, path: "/apis/memory.kubememlens.io/v1alpha1/namespaces/team-a/pods/api", verb: "get", resource: "pods", namespace: "team-a", item: "api"},
		{name: "namespaced Pod history", method: http.MethodGet, path: "/apis/memory.kubememlens.io/v1alpha1/namespaces/team-a/pods/api/history", verb: "get", resource: "pods", subresource: "history", namespace: "team-a", item: "api"},
		{name: "namespaced container list", method: http.MethodGet, path: "/apis/memory.kubememlens.io/v1alpha1/namespaces/team-a/containers", verb: "list", resource: "containers", namespace: "team-a"},
		{name: "namespaced workload list", method: http.MethodGet, path: "/apis/memory.kubememlens.io/v1alpha1/namespaces/team-a/workloads", verb: "list", resource: "workloads", namespace: "team-a"},
		{name: "cluster Pod list", method: http.MethodGet, path: "/apis/memory.kubememlens.io/v1alpha1/pods", verb: "list", resource: "pods"},
		{name: "node list", method: http.MethodGet, path: "/apis/memory.kubememlens.io/v1alpha1/nodes", verb: "list", resource: "nodes"},
		{name: "node get", method: http.MethodGet, path: "/apis/memory.kubememlens.io/v1alpha1/nodes/node-a", verb: "get", resource: "nodes", item: "node-a"},
		{name: "cluster status get", method: http.MethodGet, path: "/apis/memory.kubememlens.io/v1alpha1/clusterstatus/current", verb: "get", resource: "clusterstatus", item: "current"},
		{name: "metrics get", method: http.MethodGet, path: "/apis/memory.kubememlens.io/v1alpha1/metrics/current", verb: "get", resource: "metrics", item: "current"},
		{name: "ingestion epoch get", method: http.MethodGet, path: "/apis/memory.kubememlens.io/v1alpha1/ingestionepochs/current", verb: "get", resource: "ingestionepochs", item: "current", agent: true},
		{name: "node snapshot create", method: http.MethodPost, path: "/apis/memory.kubememlens.io/v1alpha1/nodesnapshots", verb: "create", resource: "nodesnapshots", agent: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := requestWithAuthorisationContext(t, test.method, test.path, test.agent)
			attributes, err := genericfilters.GetAuthorizerAttributes(request.Context())
			if err != nil {
				t.Fatalf("GetAuthorizerAttributes: %v", err)
			}
			delegated := 0
			gate := agentIdentityAuthorizer{
				expectedUsername: "system:serviceaccount:kube-memlens:kube-memlens-agent",
				delegate: authorizer.AuthorizerFunc(func(_ context.Context, received authorizer.Attributes) (authorizer.Decision, string, error) {
					delegated++
					if received.GetVerb() != test.verb || received.GetAPIGroup() != api.MemoryAPIGroup ||
						received.GetAPIVersion() != api.MemoryAPIVersion || received.GetResource() != test.resource ||
						received.GetSubresource() != test.subresource || received.GetNamespace() != test.namespace || received.GetName() != test.item {
						t.Fatalf("delegated attributes: verb=%q group=%q version=%q resource=%q subresource=%q namespace=%q name=%q",
							received.GetVerb(), received.GetAPIGroup(), received.GetAPIVersion(), received.GetResource(),
							received.GetSubresource(), received.GetNamespace(), received.GetName())
					}
					return authorizer.DecisionAllow, "allowed", nil
				}),
			}
			decision, _, err := gate.Authorize(request.Context(), attributes)
			if err != nil || decision != authorizer.DecisionAllow || delegated != 1 {
				t.Fatalf("decision=%v delegated=%d err=%v", decision, delegated, err)
			}
		})
	}
}

func TestDelegatedAuthorisationFailuresNeverReachStoreHandler(t *testing.T) {
	tests := []struct {
		name     string
		decision authorizer.Decision
		err      error
		status   int
	}{
		{name: "explicit deny", decision: authorizer.DecisionDeny, status: http.StatusForbidden},
		{name: "no opinion", decision: authorizer.DecisionNoOpinion, status: http.StatusForbidden},
		{name: "delegate error", decision: authorizer.DecisionNoOpinion, err: errors.New("sensitive delegate failure"), status: http.StatusInternalServerError},
		{name: "contradictory allow and error", decision: authorizer.DecisionAllow, err: errors.New("sensitive delegate failure"), status: http.StatusInternalServerError},
	}

	scheme := runtime.NewScheme()
	metav1.AddToGroupVersion(scheme, schema.GroupVersion{Version: "v1"})
	scheme.AddUnversionedTypes(schema.GroupVersion{Group: "", Version: "v1"}, &metav1.Status{})
	codecs := serializer.NewCodecFactory(scheme)
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			storeWork := 0
			delegateCalls := 0
			gate := agentIdentityAuthorizer{delegate: authorizer.AuthorizerFunc(func(context.Context, authorizer.Attributes) (authorizer.Decision, string, error) {
				delegateCalls++
				return test.decision, "bounded reason", test.err
			})}
			tail := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { storeWork++ })
			handler := genericfilters.WithAuthorization(tail, gate, codecs)
			request := requestWithAuthorisationContext(t, http.MethodGet, "/apis/memory.kubememlens.io/v1alpha1/namespaces/team-a/pods", false)
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)

			if recorder.Code != test.status || storeWork != 0 || delegateCalls != 1 {
				t.Fatalf("status=%d storeWork=%d delegateCalls=%d body=%s", recorder.Code, storeWork, delegateCalls, recorder.Body.String())
			}
			if strings.Contains(recorder.Body.String(), "sensitive delegate failure") {
				t.Fatalf("response exposed delegate error: %s", recorder.Body.String())
			}
			if test.err != nil && !strings.Contains(recorder.Body.String(), errDelegatedAuthorisation.Error()) {
				t.Fatalf("response omitted bounded delegate error: %s", recorder.Body.String())
			}
		})
	}
}

func requestWithAuthorisationContext(t *testing.T, method, path string, agent bool) *http.Request {
	t.Helper()
	request := httptest.NewRequest(method, path, nil)
	factory := &apirequest.RequestInfoFactory{
		APIPrefixes: sets.NewString("api", "apis"), GrouplessAPIPrefixes: sets.NewString("api"),
	}
	info, err := factory.NewRequestInfo(request)
	if err != nil {
		t.Fatalf("NewRequestInfo: %v", err)
	}
	principal := &user.DefaultInfo{Name: "tenant-reader", Groups: []string{"system:authenticated"}}
	if agent {
		principal = &user.DefaultInfo{
			Name: "system:serviceaccount:kube-memlens:kube-memlens-agent",
			Extra: map[string][]string{
				PodUIDExtra: {"pod-a"}, NodeNameExtra: {"node-a"}, NodeUIDExtra: {"node-uid-a"}, CredentialIDExtra: {"credential-a"},
			},
		}
	}
	ctx := apirequest.WithRequestInfo(request.Context(), info)
	ctx = apirequest.WithUser(ctx, principal)
	return request.WithContext(ctx)
}
