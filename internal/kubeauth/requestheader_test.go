package kubeauth

import (
	"context"
	"encoding/pem"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	genericapiserver "k8s.io/apiserver/pkg/server"
	"k8s.io/client-go/kubernetes/fake"
	"net/http/httptest"
	"testing"
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
	_, err := ConfigureRequestHeader(context.Background(), client, config)
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
