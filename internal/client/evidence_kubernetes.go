package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/danushkastanley/kube-memlens/internal/capability"
	"github.com/danushkastanley/kube-memlens/internal/kube"
	"github.com/danushkastanley/kube-memlens/internal/resourcemetrics"
	authorization "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
)

func discoverStatus(ctx context.Context, opts Options) (capability.SourceState, error) {
	state, err := discoverAccess(ctx, opts, "", "pods")
	state.Source, state.APIVersion, state.Stability = capability.KubernetesStatus, "v1", capability.Stable
	return state, err
}

func discoverMetrics(ctx context.Context, opts Options) (capability.SourceState, error) {
	config, err := kube.BuildConfig(opts.Kubeconfig, opts.Context)
	if err != nil {
		return evidenceFailure(err), err
	}
	// Discovery has no namespace endpoint. The source requires a valid scope
	// for its Read method, which this adapter never invokes.
	namespace := opts.ReadScope.Namespace
	if opts.ReadScope.AllNamespaces {
		namespace = "default"
	}
	source, err := resourcemetrics.New(config, resourcemetrics.Options{Namespace: namespace, Timeout: opts.Timeout})
	if err != nil {
		return evidenceFailure(err), err
	}
	report, err := source.Discover(ctx)
	state := capability.SourceState{Source: capability.KubernetesMetrics, Availability: capability.Unavailable,
		APIVersion: report.APIVersion, Freshness: capability.UnknownFreshness,
		Completeness: capability.Partial, Stability: capability.Stable}
	if strings.HasSuffix(report.APIVersion, "/v1beta1") {
		state.Stability = capability.Beta
	}
	if err != nil {
		state.Reason = capability.RequestFailed
		return state, err
	}
	switch report.Availability {
	case resourcemetrics.Available:
		access, err := discoverAccess(ctx, opts, "metrics.k8s.io", "pods")
		state.Availability, state.Reason = access.Availability, access.Reason
		return state, err
	case resourcemetrics.MissingProvider:
		state.Availability, state.Reason = capability.Absent, capability.SourceAbsent
	case resourcemetrics.Forbidden:
		state.Availability, state.Reason = capability.Forbidden, capability.AccessDenied
	default:
		state.Reason = capability.Reason(report.Reason)
		if report.Reason == resourcemetrics.UnsupportedAPI {
			state.Availability = capability.Unsupported
		}
	}
	return state, nil
}

// A self access review reveals no object identities. It is only a discovery
// hint: subsequent reads must still enforce current API-server authorisation.
func discoverAccess(ctx context.Context, opts Options, group, resource string) (capability.SourceState, error) {
	config, err := kube.BuildConfig(opts.Kubeconfig, opts.Context)
	if err != nil {
		return evidenceFailure(err), err
	}
	copied := rest.CopyConfig(config)
	copied.DisableCompression = true
	transport, err := rest.HTTPClientFor(copied)
	if err != nil {
		return evidenceFailure(err), err
	}
	client := &http.Client{Transport: transport.Transport, Timeout: opts.Timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	namespace := opts.ReadScope.Namespace
	if opts.ReadScope.AllNamespaces {
		namespace = ""
	}
	review := authorization.SelfSubjectAccessReview{
		TypeMeta: metav1.TypeMeta{APIVersion: "authorization.k8s.io/v1", Kind: "SelfSubjectAccessReview"},
		Spec: authorization.SelfSubjectAccessReviewSpec{ResourceAttributes: &authorization.ResourceAttributes{
			Namespace: namespace, Verb: "list", Group: group, Resource: resource}},
	}
	body, err := json.Marshal(review)
	if err != nil {
		return evidenceFailure(err), err
	}
	endpoint := strings.TrimRight(config.Host, "/") + "/apis/authorization.k8s.io/v1/selfsubjectaccessreviews"
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return evidenceFailure(err), err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return evidenceFailure(err), err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated && response.StatusCode != http.StatusOK {
		// Failure to evaluate access is not evidence that the source is absent.
		state := evidenceFailure(readStatusError("discover access", response.StatusCode))
		if state.Availability == capability.Absent {
			state.Availability, state.Reason = capability.Unavailable, capability.RequestFailed
		}
		return state, nil
	}
	const maxReviewBytes = 1 << 20
	data, err := io.ReadAll(io.LimitReader(response.Body, maxReviewBytes+1))
	if err != nil {
		return evidenceFailure(err), err
	}
	var result authorization.SelfSubjectAccessReview
	if len(data) > maxReviewBytes || json.Unmarshal(data, &result) != nil || result.Kind != "SelfSubjectAccessReview" || result.APIVersion != "authorization.k8s.io/v1" || result.Status.EvaluationError != "" || result.Status.Allowed && result.Status.Denied {
		return evidenceFailure(readDecodeError("discover access", fmt.Errorf("invalid access review"))), nil
	}
	state := capability.SourceState{Availability: capability.Forbidden, Reason: capability.AccessDenied,
		Freshness: capability.UnknownFreshness, Completeness: capability.Partial}
	if result.Status.Allowed {
		state.Availability, state.Reason = capability.Available, capability.NotObserved
	}
	return state, nil
}
