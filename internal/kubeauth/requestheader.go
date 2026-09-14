// Package kubeauth owns the aggregation-proxy authentication boundary shared by
// the standard read API and separately installed optional APIs.
package kubeauth

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apiserverconfig "k8s.io/apiserver/pkg/apis/apiserver"
	"k8s.io/apiserver/pkg/authentication/authenticatorfactory"
	"k8s.io/apiserver/pkg/authentication/request/headerrequest"
	genericapiserver "k8s.io/apiserver/pkg/server"
	"k8s.io/apiserver/pkg/server/dynamiccertificates"
	"k8s.io/apiserver/pkg/server/healthz"
	genericoptions "k8s.io/apiserver/pkg/server/options"
	"k8s.io/client-go/kubernetes"
	certutil "k8s.io/client-go/util/cert"
)

// ConfigureRequestHeader accepts identities only from the configured Kubernetes
// aggregation proxy. Direct bearer tokens and unauthenticated headers are not
// credentials; anonymous access is restricted to the three health endpoints.
func ConfigureRequestHeader(ctx context.Context, client kubernetes.Interface, config *genericapiserver.Config) (*genericoptions.DynamicRequestHeaderController, error) {
	if err := validateRequestHeaderConfigMap(ctx, client); err != nil {
		return nil, err
	}
	ca, err := dynamiccertificates.NewDynamicCAFromConfigMapController(
		"kube-memlens-request-header", metav1.NamespaceSystem, "extension-apiserver-authentication", "requestheader-client-ca-file", client)
	if err != nil {
		return nil, err
	}
	headers := headerrequest.NewRequestHeaderAuthRequestController(
		"extension-apiserver-authentication", metav1.NamespaceSystem, client,
		"requestheader-username-headers", "requestheader-uid-headers", "requestheader-group-headers",
		"requestheader-extra-headers-prefix", "requestheader-allowed-names")
	controller := &genericoptions.DynamicRequestHeaderController{
		ConfigMapCAController: ca, RequestHeaderAuthRequestController: headers,
	}
	if err := controller.RunOnce(ctx); err != nil {
		return nil, fmt.Errorf("load request-header authentication configuration: %w", err)
	}
	requestHeaderConfig := &authenticatorfactory.RequestHeaderConfig{
		CAContentProvider:   controller,
		UsernameHeaders:     headerrequest.StringSliceProviderFunc(controller.UsernameHeaders),
		UIDHeaders:          headerrequest.StringSliceProviderFunc(controller.UIDHeaders),
		GroupHeaders:        headerrequest.StringSliceProviderFunc(controller.GroupHeaders),
		ExtraHeaderPrefixes: headerrequest.StringSliceProviderFunc(controller.ExtraHeaderPrefixes),
		AllowedClientNames:  headerrequest.StringSliceProviderFunc(requiredAllowedNames(controller.AllowedClientNames)),
	}
	authenticator, _, err := (authenticatorfactory.DelegatingAuthenticatorConfig{
		Anonymous: &apiserverconfig.AnonymousAuthConfig{Enabled: true, Conditions: []apiserverconfig.AnonymousAuthCondition{
			{Path: "/healthz"}, {Path: "/livez"}, {Path: "/readyz"},
		}},
		RequestHeaderConfig: requestHeaderConfig,
	}).New()
	if err != nil {
		return nil, err
	}
	config.Authentication.Authenticator = authenticator
	config.Authentication.RequestHeaderConfig = requestHeaderConfig
	if err := config.Authentication.ApplyClientCert(controller, config.SecureServing); err != nil {
		return nil, err
	}
	return controller, nil
}

func validateRequestHeaderConfigMap(ctx context.Context, client kubernetes.Interface) error {
	configMap, err := client.CoreV1().ConfigMaps(metav1.NamespaceSystem).Get(ctx, "extension-apiserver-authentication", metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("read extension authentication ConfigMap: %w", err)
	}
	if certs, err := certutil.ParseCertsPEM([]byte(configMap.Data["requestheader-client-ca-file"])); err != nil || len(certs) == 0 {
		return fmt.Errorf("request-header client CA is missing or invalid")
	}
	for _, key := range []string{
		"requestheader-username-headers", "requestheader-group-headers",
		"requestheader-extra-headers-prefix", "requestheader-allowed-names",
	} {
		if strings.TrimSpace(configMap.Data[key]) == "" {
			return fmt.Errorf("request-header configuration %s is missing", key)
		}
	}
	return nil
}

func requiredAllowedNames(source func() []string) func() []string {
	return func() []string {
		names := source()
		if len(names) == 0 {
			return []string{"\x00invalid-empty-proxy-cn"}
		}
		return names
	}
}

// RequestHeaderReady prevents readiness when the dynamic proxy trust settings
// have become unavailable.
func RequestHeaderReady(controller *genericoptions.DynamicRequestHeaderController) healthz.HealthChecker {
	return healthz.NamedCheck("request-header-config", func(_ *http.Request) error {
		if len(controller.CurrentCABundleContent()) == 0 || len(controller.UsernameHeaders()) == 0 ||
			len(controller.GroupHeaders()) == 0 || len(controller.ExtraHeaderPrefixes()) == 0 || len(controller.AllowedClientNames()) == 0 {
			return fmt.Errorf("request-header authentication configuration is unavailable")
		}
		return nil
	})
}
