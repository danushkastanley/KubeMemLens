package extension

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	authorizationv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apiserver/pkg/server/healthz"
	typedauthorizationv1 "k8s.io/client-go/kubernetes/typed/authorization/v1"
)

const (
	delegatedSARProbeInterval = 10 * time.Second
	delegatedSARProbeTimeout  = 2 * time.Second
)

var errDelegatedAuthorisationUnavailable = errors.New("delegated authorisation connectivity is unavailable")

// delegatedSARReadiness periodically checks the Kubernetes authorisation API.
// Check only reads cached state so kubelet traffic cannot amplify SAR load.
type delegatedSARReadiness struct {
	client   typedauthorizationv1.SubjectAccessReviewInterface
	interval time.Duration
	timeout  time.Duration

	mu    sync.RWMutex
	ready bool
}

func newDelegatedSARReadiness(client typedauthorizationv1.SubjectAccessReviewInterface) *delegatedSARReadiness {
	return &delegatedSARReadiness{
		client: client, interval: delegatedSARProbeInterval, timeout: delegatedSARProbeTimeout,
	}
}

func (p *delegatedSARReadiness) Name() string {
	return "delegated-authorisation"
}

func (p *delegatedSARReadiness) Check(*http.Request) error {
	p.mu.RLock()
	ready := p.ready
	p.mu.RUnlock()
	if !ready {
		return errDelegatedAuthorisationUnavailable
	}
	return nil
}

func (p *delegatedSARReadiness) Run(ctx context.Context) {
	p.probe(ctx)
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.probe(ctx)
		}
	}
}

func (p *delegatedSARReadiness) probe(ctx context.Context) {
	probeCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	result, err := p.client.Create(probeCtx, delegatedSAR(), metav1.CreateOptions{})
	ready := err == nil && result != nil && result.Status.EvaluationError == ""
	p.mu.Lock()
	p.ready = ready
	p.mu.Unlock()
}

func delegatedSAR() *authorizationv1.SubjectAccessReview {
	return &authorizationv1.SubjectAccessReview{
		Spec: authorizationv1.SubjectAccessReviewSpec{
			User:   "system:kube-memlens-readiness",
			Groups: []string{"system:authenticated"},
			ResourceAttributes: &authorizationv1.ResourceAttributes{
				Verb: "get", Group: api.MemoryAPIGroup, Version: api.MemoryAPIVersion,
				Resource: "clusterstatus", Name: "current",
			},
		},
	}
}

var _ healthz.HealthChecker = (*delegatedSARReadiness)(nil)
