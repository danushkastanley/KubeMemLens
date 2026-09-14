// Package admissionkube adapts current Kubernetes policy and object reads to the
// optional admission contract. It is not imported by the standard agent.
package admissionkube

import (
	"context"

	admission "github.com/danushkastanley/kube-memlens/internal/traceadmission"
	authorizationv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apiserver/pkg/authentication/user"
	authclient "k8s.io/client-go/kubernetes/typed/authorization/v1"
)

type Authorizer struct {
	reviews authclient.SubjectAccessReviewInterface
}

func NewAuthorizer(reviews authclient.SubjectAccessReviewInterface) *Authorizer {
	return &Authorizer{reviews: reviews}
}

func (a *Authorizer) Trace(ctx context.Context, p user.Info, operation admission.Operation, namespace, name string) error {
	resource := &authorizationv1.ResourceAttributes{Namespace: namespace, Verb: string(operation), Group: admission.APIGroup, Version: admission.APIVersion, Resource: "traces", Name: name}
	switch operation {
	case admission.Create, admission.Read, admission.Cancel:
	case admission.Attach:
		resource.Verb = "get"
		resource.Subresource = "stream"
	default:
		return admission.ErrInvalidRequest
	}
	return a.authorize(ctx, p, resource)
}
func (a *Authorizer) Pod(ctx context.Context, p user.Info, namespace, name string) error {
	return a.authorize(ctx, p, &authorizationv1.ResourceAttributes{Namespace: namespace, Verb: "get", Version: "v1", Resource: "pods", Name: name})
}
func (a *Authorizer) authorize(ctx context.Context, p user.Info, resource *authorizationv1.ResourceAttributes) error {
	if a.reviews == nil || p == nil || ctx.Err() != nil {
		return admission.ErrUnavailable
	}
	extras := map[string]authorizationv1.ExtraValue{}
	for key, values := range p.GetExtra() {
		extras[key] = append(authorizationv1.ExtraValue(nil), values...)
	}
	response, err := a.reviews.Create(ctx, &authorizationv1.SubjectAccessReview{Spec: authorizationv1.SubjectAccessReviewSpec{User: p.GetName(), UID: p.GetUID(), Groups: append([]string(nil), p.GetGroups()...), Extra: extras, ResourceAttributes: resource}}, metav1.CreateOptions{})
	if err != nil || response == nil || response.Status.EvaluationError != "" {
		return admission.ErrUnavailable
	}
	if !response.Status.Allowed || response.Status.Denied {
		return admission.ErrDenied
	}
	return nil
}
