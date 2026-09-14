package admissionapi

import (
	"context"
	"errors"
	"time"

	admission "github.com/danushkastanley/kube-memlens/internal/traceadmission"
	authorizationv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apiserver/pkg/authorization/authorizer"
	authclient "k8s.io/client-go/kubernetes/typed/authorization/v1"
)

type delegatedAuthorizer struct {
	reviews authclient.SubjectAccessReviewInterface
}

func (a delegatedAuthorizer) ConditionsAwareAuthorize(ctx context.Context, attrs authorizer.Attributes) authorizer.ConditionsAwareDecision {
	return authorizer.ConditionsAwareDecisionFromParts(a.Authorize(ctx, attrs))
}
func (a delegatedAuthorizer) EvaluateConditions(context.Context, authorizer.ConditionsAwareDecision, authorizer.ConditionsData) (authorizer.Decision, string, error) {
	return authorizer.DecisionDeny, "", authorizer.ErrorConditionEvaluationNotSupported
}
func (a delegatedAuthorizer) Authorize(ctx context.Context, attrs authorizer.Attributes) (authorizer.Decision, string, error) {
	if !attrs.IsResourceRequest() && attrs.GetVerb() == "get" {
		switch attrs.GetPath() {
		case "/healthz", "/livez", "/readyz":
			return authorizer.DecisionAllow, "health", nil
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	p := attrs.GetUser()
	if p == nil || a.reviews == nil {
		return authorizer.DecisionDeny, "unavailable", errors.New("authorisation unavailable")
	}
	spec := authorizationv1.SubjectAccessReviewSpec{User: p.GetName(), UID: p.GetUID(), Groups: append([]string(nil), p.GetGroups()...), Extra: map[string]authorizationv1.ExtraValue{}}
	for k, v := range p.GetExtra() {
		spec.Extra[k] = append(authorizationv1.ExtraValue(nil), v...)
	}
	if attrs.IsResourceRequest() {
		// No optional route can accidentally expand this resource vocabulary.
		if attrs.GetAPIGroup() != admission.APIGroup || attrs.GetAPIVersion() != admission.APIVersion || attrs.GetResource() != "traces" || (attrs.GetSubresource() != "" && (attrs.GetSubresource() != "stream" || attrs.GetVerb() != "get" || attrs.GetName() == "")) || attrs.GetNamespace() == "" {
			return authorizer.DecisionDeny, "denied", nil
		}
		spec.ResourceAttributes = &authorizationv1.ResourceAttributes{Group: attrs.GetAPIGroup(), Version: attrs.GetAPIVersion(), Resource: attrs.GetResource(), Namespace: attrs.GetNamespace(), Name: attrs.GetName(), Verb: attrs.GetVerb(), Subresource: attrs.GetSubresource()}
	} else {
		spec.NonResourceAttributes = &authorizationv1.NonResourceAttributes{Path: attrs.GetPath(), Verb: attrs.GetVerb()}
	}
	result, err := a.reviews.Create(ctx, &authorizationv1.SubjectAccessReview{Spec: spec}, metav1.CreateOptions{})
	if err != nil || result == nil || result.Status.EvaluationError != "" {
		return authorizer.DecisionDeny, "unavailable", errors.New("authorisation unavailable")
	}
	if !result.Status.Allowed || result.Status.Denied {
		return authorizer.DecisionDeny, "denied", nil
	}
	return authorizer.DecisionAllow, "allowed", nil
}
