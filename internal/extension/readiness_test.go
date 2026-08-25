package extension

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	authorizationv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
)

func TestDelegatedSARReadinessCachesSuccessfulEvaluation(t *testing.T) {
	calls := 0
	client := fake.NewSimpleClientset()
	client.PrependReactor("create", "subjectaccessreviews", func(action clienttesting.Action) (bool, runtime.Object, error) {
		calls++
		created := action.(clienttesting.CreateAction).GetObject().(*authorizationv1.SubjectAccessReview)
		attributes := created.Spec.ResourceAttributes
		if created.Spec.User != "system:kube-memlens-readiness" || len(created.Spec.Groups) != 1 ||
			attributes == nil || attributes.Verb != "get" || attributes.Group != api.MemoryAPIGroup ||
			attributes.Version != api.MemoryAPIVersion || attributes.Resource != "clusterstatus" || attributes.Name != "current" {
			t.Fatalf("unexpected readiness SAR: %#v", created.Spec)
		}
		return true, &authorizationv1.SubjectAccessReview{
			Status: authorizationv1.SubjectAccessReviewStatus{Allowed: false, Reason: "expected denial"},
		}, nil
	})
	probe := newDelegatedSARReadiness(client.AuthorizationV1().SubjectAccessReviews())

	if err := probe.Check(nil); !errors.Is(err, errDelegatedAuthorisationUnavailable) {
		t.Fatalf("initial Check() error = %v", err)
	}
	probe.probe(context.Background())
	if err := probe.Check(nil); err != nil {
		t.Fatalf("Check() after evaluated denial = %v", err)
	}
	if err := probe.Check(nil); err != nil {
		t.Fatalf("cached Check() = %v", err)
	}
	if calls != 1 {
		t.Fatalf("SAR calls = %d, want 1", calls)
	}
}

func TestDelegatedSARReadinessFailsClosedAndRecovers(t *testing.T) {
	response := 0
	client := fake.NewSimpleClientset()
	client.PrependReactor("create", "subjectaccessreviews", func(clienttesting.Action) (bool, runtime.Object, error) {
		response++
		switch response {
		case 1:
			return true, nil, errors.New("sensitive transport failure")
		case 2:
			return true, &authorizationv1.SubjectAccessReview{
				Status: authorizationv1.SubjectAccessReviewStatus{EvaluationError: "sensitive evaluator failure"},
			}, nil
		default:
			return true, &authorizationv1.SubjectAccessReview{
				Status: authorizationv1.SubjectAccessReviewStatus{Denied: true},
			}, nil
		}
	})
	probe := newDelegatedSARReadiness(client.AuthorizationV1().SubjectAccessReviews())

	for range 2 {
		probe.probe(context.Background())
		if err := probe.Check(nil); !errors.Is(err, errDelegatedAuthorisationUnavailable) ||
			err.Error() != "delegated authorisation connectivity is unavailable" {
			t.Fatalf("Check() error = %v", err)
		}
	}
	probe.probe(context.Background())
	if err := probe.Check(nil); err != nil {
		t.Fatalf("Check() after recovery = %v", err)
	}
}

func TestDelegatedSARReadinessBoundsTransportWork(t *testing.T) {
	probe := newDelegatedSARReadiness(blockingSubjectAccessReviewClient{})
	probe.timeout = 10 * time.Millisecond

	started := time.Now()
	probe.probe(context.Background())
	if elapsed := time.Since(started); elapsed > 250*time.Millisecond {
		t.Fatalf("probe took %s", elapsed)
	}
	if err := probe.Check(nil); !errors.Is(err, errDelegatedAuthorisationUnavailable) {
		t.Fatalf("Check() error = %v", err)
	}
}

type blockingSubjectAccessReviewClient struct{}

func (blockingSubjectAccessReviewClient) Create(ctx context.Context, _ *authorizationv1.SubjectAccessReview, _ metav1.CreateOptions) (*authorizationv1.SubjectAccessReview, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}
