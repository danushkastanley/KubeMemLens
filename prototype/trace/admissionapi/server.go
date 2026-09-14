package admissionapi

import (
	"context"
	"errors"
	"golang.org/x/net/netutil"
	apirequest "k8s.io/apiserver/pkg/endpoints/request"
	"net/http"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/kubeauth"
	admission "github.com/danushkastanley/kube-memlens/internal/traceadmission"
	apidiscoveryv2 "k8s.io/api/apidiscovery/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	apiendpoints "k8s.io/apiserver/pkg/endpoints"
	genericapiserver "k8s.io/apiserver/pkg/server"
	genericoptions "k8s.io/apiserver/pkg/server/options"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/client-go/kubernetes"
	basecompatibility "k8s.io/component-base/compatibility"
)

type ServerOptions struct {
	Port                     int
	CertificateFile, KeyFile string
	Client                   kubernetes.Interface
	Manager                  *admission.Manager
	Stream                   *StreamProxy
}

func (o ServerOptions) Run(ctx context.Context) error {
	if o.Port < 1 || o.Port > 65535 || o.CertificateFile == "" || o.KeyFile == "" || o.Client == nil || o.Manager == nil {
		return errors.New("trace API configuration is incomplete")
	}
	scheme := runtime.NewScheme()
	metav1.AddToGroupVersion(scheme, schema.GroupVersion{Version: "v1"})
	scheme.AddUnversionedTypes(schema.GroupVersion{Version: "v1"}, &metav1.Status{}, &metav1.APIVersions{}, &metav1.APIGroupList{}, &metav1.APIGroup{}, &metav1.APIResourceList{})
	config := genericapiserver.NewConfig(serializer.NewCodecFactory(scheme))
	config.EnableProfiling = false
	config.EnableMetrics = false
	config.MaxRequestBodyBytes = admission.MaxRequestBytes
	config.MaxRequestsInFlight = 8
	config.MaxMutatingRequestsInFlight = 4
	config.RequestTimeout = 10 * time.Second
	config.LongRunningFunc = func(r *http.Request, info *apirequest.RequestInfo) bool {
		return info != nil && r.Method == http.MethodGet && info.APIGroup == admission.APIGroup && info.APIVersion == admission.APIVersion && info.Resource == "traces" && info.Subresource == "stream" && info.Name != "" && info.Namespace != ""
	}
	config.ShutdownDelayDuration = time.Second
	config.FeatureGate = utilfeature.DefaultFeatureGate
	config.EffectiveVersion = basecompatibility.NewEffectiveVersionFromString("1.37", "", "")
	secure := genericoptions.NewSecureServingOptions().WithLoopback()
	secure.BindPort = o.Port
	secure.MinTLSVersion = "VersionTLS13"
	secure.DisableHTTP2Serving = true
	secure.ServerCert.CertKey.CertFile = o.CertificateFile
	secure.ServerCert.CertKey.KeyFile = o.KeyFile
	if err := secure.ApplyTo(&config.SecureServing, &config.LoopbackClientConfig); err != nil {
		return errors.New("configure trace API TLS failed")
	}
	config.SecureServing.Listener = netutil.LimitListener(config.SecureServing.Listener, 64)
	defer config.SecureServing.Listener.Close()
	headers, err := kubeauth.ConfigureRequestHeader(ctx, o.Client, config)
	if err != nil {
		return errors.New("configure trace API authentication failed")
	}
	config.AddReadyzChecks(kubeauth.RequestHeaderReady(headers))
	config.Authorization.Authorizer = delegatedAuthorizer{o.Client.AuthorizationV1().SubjectAccessReviews()}
	server, err := config.Complete(nil).New("kube-memlens-trace-admission", genericapiserver.NewEmptyDelegate())
	if err != nil {
		return errors.New("create trace API failed")
	}
	discovery, err := apiendpoints.ConvertGroupVersionIntoToDiscovery(resources())
	if err != nil {
		return err
	}
	server.AggregatedDiscoveryGroupManager.AddGroupVersion(admission.APIGroup, apidiscoveryv2.APIVersionDiscovery{Version: admission.APIVersion, Resources: discovery, Freshness: apidiscoveryv2.DiscoveryFreshnessCurrent})
	server.AggregatedDiscoveryGroupManager.SetGroupVersionPriority(metav1.GroupVersion{Group: admission.APIGroup, Version: admission.APIVersion}, 1000, 15)
	handler := NewHandler(o.Manager)
	handler.stream = o.Stream
	server.Handler.NonGoRestfulMux.Handle(prefix, handler)
	server.Handler.NonGoRestfulMux.HandlePrefix(prefix+"/", handler)
	return server.PrepareRun().RunWithContext(ctx)
}
