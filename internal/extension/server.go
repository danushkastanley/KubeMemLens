package extension

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/buildinfo"
	"github.com/danushkastanley/kube-memlens/internal/kube"
	apidiscoveryv2 "k8s.io/api/apidiscovery/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	apiserverconfig "k8s.io/apiserver/pkg/apis/apiserver"
	"k8s.io/apiserver/pkg/audit"
	"k8s.io/apiserver/pkg/authentication/authenticatorfactory"
	"k8s.io/apiserver/pkg/authentication/request/headerrequest"
	"k8s.io/apiserver/pkg/authentication/user"
	"k8s.io/apiserver/pkg/authorization/authorizer"
	apiendpoints "k8s.io/apiserver/pkg/endpoints"
	genericapiserver "k8s.io/apiserver/pkg/server"
	"k8s.io/apiserver/pkg/server/dynamiccertificates"
	"k8s.io/apiserver/pkg/server/healthz"
	genericoptions "k8s.io/apiserver/pkg/server/options"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/client-go/kubernetes"
	certutil "k8s.io/client-go/util/cert"
	basecompatibility "k8s.io/component-base/compatibility"
)

var errDelegatedAuthorisation = errors.New("delegated authorisation failed")

const defaultShutdownDelay = 3 * time.Second

type ServerOptions struct {
	BindPort        int
	CertFile        string
	KeyFile         string
	KubeconfigFile  string
	MaxBodyBytes    int64
	MaxRead         int
	MaxMutating     int
	RequestTimeout  time.Duration
	ShutdownDelay   time.Duration
	NodeSelector    string
	NodeTolerations []corev1.Toleration
	Handler         *Handler
}

func (o ServerOptions) Run(ctx context.Context) error {
	if o.Handler == nil {
		return fmt.Errorf("extension handler is required")
	}
	if o.BindPort <= 0 || o.CertFile == "" || o.KeyFile == "" {
		return fmt.Errorf("extension TLS port, certificate and key are required")
	}
	if o.MaxBodyBytes <= 0 || o.MaxRead <= 0 || o.MaxMutating <= 0 || o.RequestTimeout <= 0 {
		return fmt.Errorf("extension request limits must be greater than zero")
	}
	if o.ShutdownDelay < 0 {
		return fmt.Errorf("extension shutdown delay cannot be negative")
	}
	shutdownDelay := o.ShutdownDelay
	if shutdownDelay == 0 {
		shutdownDelay = defaultShutdownDelay
	}

	scheme := runtime.NewScheme()
	metav1.AddToGroupVersion(scheme, schema.GroupVersion{Version: "v1"})
	scheme.AddUnversionedTypes(schema.GroupVersion{Group: "", Version: "v1"},
		&metav1.Status{}, &metav1.APIVersions{}, &metav1.APIGroupList{}, &metav1.APIGroup{}, &metav1.APIResourceList{})
	codecs := serializer.NewCodecFactory(scheme)
	config := genericapiserver.NewConfig(codecs)
	config.EnableProfiling = false
	config.EnableMetrics = false
	config.MaxRequestBodyBytes = o.MaxBodyBytes
	config.MaxRequestsInFlight = o.MaxRead
	config.MaxMutatingRequestsInFlight = o.MaxMutating
	config.RequestTimeout = o.RequestTimeout
	config.ShutdownDelayDuration = shutdownDelay
	config.FeatureGate = utilfeature.DefaultFeatureGate
	config.EffectiveVersion = basecompatibility.NewEffectiveVersionFromString("1.33", "", "")

	secure := genericoptions.NewSecureServingOptions().WithLoopback()
	secure.BindPort = o.BindPort
	secure.ServerCert.CertKey.CertFile = o.CertFile
	secure.ServerCert.CertKey.KeyFile = o.KeyFile
	if err := secure.ApplyTo(&config.SecureServing, &config.LoopbackClientConfig); err != nil {
		return fmt.Errorf("configure extension TLS: %w", err)
	}

	requestHeaders, client, err := configureRequestHeaderAuthentication(ctx, o.KubeconfigFile, config)
	if err != nil {
		return fmt.Errorf("configure delegated authentication: %w", err)
	}
	delegatedReadiness := newDelegatedSARReadiness(client.AuthorizationV1().SubjectAccessReviews())
	nodeCoverage := newNodeCoverageReadiness(
		client.CoreV1().Nodes(), o.Handler.coordinator.store, o.Handler.coordinator.store.MaxNodes(), o.NodeSelector, o.NodeTolerations,
	)
	probeCtx, stopProbe := context.WithCancel(ctx)
	defer stopProbe()
	go delegatedReadiness.Run(probeCtx)
	go nodeCoverage.Run(probeCtx)
	config.AddReadyzChecks(requestHeaderReady(requestHeaders), delegatedReadiness, nodeCoverage)

	authorization := genericoptions.NewDelegatingAuthorizationOptions()
	authorization.RemoteKubeConfigFile = o.KubeconfigFile
	authorization.AlwaysAllowGroups = nil
	// Health responses contain no collector data. Authorise these exact paths
	// locally so kubelet traffic cannot create a SubjectAccessReview per probe.
	authorization.AlwaysAllowPaths = delegatedAlwaysAllowPaths()
	authorization.AllowCacheTTL = 0
	authorization.DenyCacheTTL = 0
	authorization.WithClientTimeout(5 * time.Second)
	if err := authorization.ApplyTo(&config.Authorization); err != nil {
		return fmt.Errorf("configure delegated authorisation: %w", err)
	}
	config.Authorization.Authorizer = agentIdentityAuthorizer{
		expectedUsername: o.Handler.opts.AgentUsername,
		delegate:         config.Authorization.Authorizer,
		logf:             o.Handler.opts.Logf,
	}

	server, err := config.Complete(nil).New("kube-memlens-extension", genericapiserver.NewEmptyDelegate())
	if err != nil {
		return fmt.Errorf("create extension server: %w", err)
	}
	discovery, err := apiendpoints.ConvertGroupVersionIntoToDiscovery(aggregatedDiscoveryResources())
	if err != nil {
		return fmt.Errorf("build aggregated discovery: %w", err)
	}
	groupVersion := metav1.GroupVersion{Group: api.MemoryAPIGroup, Version: api.MemoryAPIVersion}
	server.AggregatedDiscoveryGroupManager.AddGroupVersion(api.MemoryAPIGroup, apidiscoveryv2.APIVersionDiscovery{
		Version: api.MemoryAPIVersion, Resources: discovery, Freshness: apidiscoveryv2.DiscoveryFreshnessCurrent,
	})
	server.AggregatedDiscoveryGroupManager.SetGroupVersionPriority(groupVersion, 1000, 15)
	o.Handler.Register(server.Handler.NonGoRestfulMux)
	return server.PrepareRun().RunWithContext(ctx)
}

func delegatedAlwaysAllowPaths() []string {
	return []string{"/healthz", "/livez", "/readyz"}
}

type agentIdentityAuthorizer struct {
	expectedUsername string
	delegate         authorizer.Authorizer
	logf             func(string, ...any)
}

func (a agentIdentityAuthorizer) Authorize(ctx context.Context, attributes authorizer.Attributes) (authorizer.Decision, string, error) {
	resource := attributes.GetResource()
	if attributes.GetAPIGroup() == api.MemoryAPIGroup && (resource == "ingestionepochs" || resource == "nodesnapshots") {
		if _, err := claimsFromUser(attributes.GetUser(), a.expectedUsername); err != nil {
			a.record(ctx, attributes, authorizer.DecisionDeny, "agent_identity")
			return authorizer.DecisionDeny, "agent identity is invalid", nil
		}
	}
	decision, reason, err := a.delegate.Authorize(ctx, attributes)
	if err != nil {
		decision = authorizer.DecisionNoOpinion
		reason = errDelegatedAuthorisation.Error()
		err = errDelegatedAuthorisation
	}
	a.record(ctx, attributes, decision, authorisationReason(decision, err))
	return decision, reason, err
}

func (a agentIdentityAuthorizer) record(ctx context.Context, attributes authorizer.Attributes, decision authorizer.Decision, reason string) {
	if a.logf == nil || attributes.GetAPIGroup() != api.MemoryAPIGroup {
		return
	}
	scope := "cluster"
	if attributes.GetNamespace() != "" {
		scope = "namespace"
	}
	requestID := audit.GetAuditIDTruncated(ctx)
	if !validRequestID(requestID) {
		requestID = "unavailable"
	}
	a.logf(
		"security request_id=%s principal=%s verb=%s resource=%s scope=%s decision=%s reason=%s",
		requestID, principalType(attributes.GetUser()), boundedVerb(attributes.GetVerb()),
		boundedResource(attributes.GetResource()), scope, boundedDecision(decision), reason,
	)
}

func authorisationReason(decision authorizer.Decision, err error) string {
	if err != nil {
		return "sar_error"
	}
	switch decision {
	case authorizer.DecisionAllow:
		return "sar_allowed"
	case authorizer.DecisionDeny:
		return "sar_denied"
	default:
		return "sar_no_opinion"
	}
}

func principalType(info user.Info) string {
	if info != nil && strings.HasPrefix(info.GetName(), "system:serviceaccount:") {
		return "serviceaccount"
	}
	return "user"
}

func boundedVerb(verb string) string {
	switch verb {
	case "get", "list", "create":
		return verb
	default:
		return "unknown"
	}
}

func boundedResource(resource string) string {
	switch resource {
	case "pods", "containers", "workloads", "nodes", "clusterstatus", "metrics", "ingestionepochs", "nodesnapshots":
		return resource
	default:
		return "unknown"
	}
}

func boundedDecision(decision authorizer.Decision) string {
	switch decision {
	case authorizer.DecisionAllow:
		return "allow"
	case authorizer.DecisionDeny:
		return "deny"
	default:
		return "no_opinion"
	}
}

func configureRequestHeaderAuthentication(ctx context.Context, kubeconfigFile string, config *genericapiserver.Config) (*genericoptions.DynamicRequestHeaderController, kubernetes.Interface, error) {
	restConfig, err := kube.BuildConfig(kubeconfigFile, "")
	if err != nil {
		return nil, nil, err
	}
	restConfig.UserAgent = "kube-memlens-extension/" + buildinfo.Version
	client, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, nil, fmt.Errorf("create Kubernetes authentication client: %w", err)
	}
	controller, err := configureRequestHeaderAuthenticationWithClient(ctx, client, config)
	return controller, client, err
}

func configureRequestHeaderAuthenticationWithClient(ctx context.Context, client kubernetes.Interface, config *genericapiserver.Config) (*genericoptions.DynamicRequestHeaderController, error) {
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

func requestHeaderReady(controller *genericoptions.DynamicRequestHeaderController) healthz.HealthChecker {
	return healthz.NamedCheck("request-header-config", func(_ *http.Request) error {
		if len(controller.CurrentCABundleContent()) == 0 || len(controller.UsernameHeaders()) == 0 ||
			len(controller.GroupHeaders()) == 0 || len(controller.ExtraHeaderPrefixes()) == 0 || len(controller.AllowedClientNames()) == 0 {
			return fmt.Errorf("request-header authentication configuration is unavailable")
		}
		return nil
	})
}
