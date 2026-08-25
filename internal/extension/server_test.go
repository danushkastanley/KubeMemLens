package extension

import (
	"context"
	"encoding/pem"
	"net/http/httptest"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/api"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apiserver/pkg/authentication/user"
	"k8s.io/apiserver/pkg/authorization/authorizer"
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
