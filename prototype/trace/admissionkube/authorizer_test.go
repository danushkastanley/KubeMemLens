package admissionkube

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	admission "github.com/danushkastanley/kube-memlens/internal/traceadmission"
	authorizationv1 "k8s.io/api/authorization/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apiserver/pkg/authentication/user"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

func TestAccessReviewsPreservePrincipalAndExactResourceAttributes(t *testing.T) {
	client := fake.NewSimpleClientset()
	reviews := []authorizationv1.SubjectAccessReviewSpec{}
	client.PrependReactor("create", "subjectaccessreviews", func(action ktesting.Action) (bool, runtime.Object, error) {
		r := action.(ktesting.CreateAction).GetObject().(*authorizationv1.SubjectAccessReview)
		reviews = append(reviews, r.Spec)
		return true, &authorizationv1.SubjectAccessReview{Status: authorizationv1.SubjectAccessReviewStatus{Allowed: true}}, nil
	})
	a := NewAuthorizer(client.AuthorizationV1().SubjectAccessReviews())
	p := &user.DefaultInfo{Name: "user-a", UID: "identity-a", Groups: []string{user.AllAuthenticated, "team-a"}, Extra: map[string][]string{"scope": {"claim-a"}}}
	for _, op := range []admission.Operation{admission.Create, admission.Read, admission.Cancel} {
		name := ""
		if op != admission.Create {
			name = "request-id"
		}
		if err := a.Trace(context.Background(), p, op, "tenant-a", name); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.Pod(context.Background(), p, "tenant-a", "selected-pod"); err != nil {
		t.Fatal(err)
	}
	for i, r := range reviews {
		if r.User != p.Name || r.UID != p.UID || !reflect.DeepEqual(r.Groups, p.Groups) || !reflect.DeepEqual([]string(r.Extra["scope"]), p.Extra["scope"]) {
			t.Fatal("delegated identity changed")
		}
		attr := r.ResourceAttributes
		if attr.Namespace != "tenant-a" || r.NonResourceAttributes != nil {
			t.Fatal("namespace or resource scope lost")
		}
		if i < 3 {
			if attr.Group != admission.APIGroup || attr.Version != admission.APIVersion || attr.Resource != "traces" {
				t.Fatal("trace route differs from SAR")
			}
			expectedName := "request-id"
			if i == 0 {
				expectedName = ""
			}
			if attr.Name != expectedName {
				t.Fatal("individual trace name omitted from SAR")
			}
		} else if attr.Group != "" || attr.Resource != "pods" || attr.Verb != "get" || attr.Version != "v1" || attr.Name != "selected-pod" {
			t.Fatal("Pod read was not exact")
		}
	}
}

func TestRevocationIsNotCachedAndIndeterminatePolicyFailsClosed(t *testing.T) {
	for _, scenario := range []string{"denied", "conflicting", "evaluation-error", "transport-error"} {
		t.Run(scenario, func(t *testing.T) {
			client := fake.NewSimpleClientset()
			calls := 0
			client.PrependReactor("create", "subjectaccessreviews", func(ktesting.Action) (bool, runtime.Object, error) {
				calls++
				status := authorizationv1.SubjectAccessReviewStatus{Allowed: true}
				if calls > 1 {
					switch scenario {
					case "denied":
						status.Allowed = false
					case "conflicting":
						status.Denied = true
					case "evaluation-error":
						status.EvaluationError = "private policy detail"
					case "transport-error":
						return true, nil, errors.New("private credential")
					}
				}
				return true, &authorizationv1.SubjectAccessReview{Status: status}, nil
			})
			a := NewAuthorizer(client.AuthorizationV1().SubjectAccessReviews())
			p := &user.DefaultInfo{Name: "user-a", Groups: []string{user.AllAuthenticated}}
			if err := a.Pod(context.Background(), p, "tenant-a", "pod"); err != nil {
				t.Fatal(err)
			}
			err := a.Pod(context.Background(), p, "tenant-a", "pod")
			if err == nil || calls != 2 || strings.Contains(err.Error(), "private") {
				t.Fatal("policy was cached, failed open or leaked details")
			}
		})
	}
}

func TestStreamAttachmentUsesExactGetSubresourceReview(t *testing.T) {
	client := fake.NewSimpleClientset()
	calls := 0
	client.PrependReactor("create", "subjectaccessreviews", func(action ktesting.Action) (bool, runtime.Object, error) {
		calls++
		review := action.(ktesting.CreateAction).GetObject().(*authorizationv1.SubjectAccessReview)
		expected := &authorizationv1.ResourceAttributes{Namespace: "tenant-a", Verb: "get", Group: admission.APIGroup, Version: admission.APIVersion, Resource: "traces", Subresource: "stream", Name: "exact-admission"}
		if !reflect.DeepEqual(review.Spec.ResourceAttributes, expected) {
			t.Fatal("stream authorisation widened resource scope")
		}
		return true, &authorizationv1.SubjectAccessReview{Status: authorizationv1.SubjectAccessReviewStatus{Allowed: true}}, nil
	})
	a := NewAuthorizer(client.AuthorizationV1().SubjectAccessReviews())
	if err := a.Trace(context.Background(), &user.DefaultInfo{Name: "owner", Groups: []string{user.AllAuthenticated}}, admission.Attach, "tenant-a", "exact-admission"); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatal("stream policy was not evaluated")
	}
}
