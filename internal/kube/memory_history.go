package kube

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/changemarkers"
	"github.com/danushkastanley/kube-memlens/internal/memoryhistory"
	"github.com/danushkastanley/kube-memlens/internal/volumehealth"
	"golang.org/x/time/rate"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/rest"
)

type memoryHistoryResolver struct {
	reader       *volumeHealthReader
	authorize    ObjectAuthorizer
	nodeIdentity VolumeNodeIdentity
	calls        *rate.Limiter
	gate         chan struct{}
	subresources []string
	workload     func(*volumeBindingQuery, context.Context, *memoryhistory.Selection, VolumeNodeIdentity) error
}

// NewMemoryHistoryResolver shares bounded object and owner-chain parsing with
// volume reads, but never requests a PVC, PV, CSI or other volume resource.
func NewMemoryHistoryResolver(config *rest.Config, authorize ObjectAuthorizer, nodeIdentity VolumeNodeIdentity) (memoryhistory.Resolver, error) {
	return newMemoryHistoryResolver(config, authorize, nodeIdentity, []string{"trends"}, (*volumeBindingQuery).historyWorkload)
}

// Context reads require both the new named permission and the existing history
// permission, before any source acquisition and again before disclosure.
func NewMemoryHistoryContextResolver(config *rest.Config, authorize ObjectAuthorizer, nodeIdentity VolumeNodeIdentity) (memoryhistory.Resolver, error) {
	return newMemoryHistoryResolver(config, authorize, nodeIdentity, []string{"trends-context", "trends"}, (*volumeBindingQuery).historyContextWorkload)
}

func newMemoryHistoryResolver(config *rest.Config, authorize ObjectAuthorizer, nodeIdentity VolumeNodeIdentity, subresources []string, workload func(*volumeBindingQuery, context.Context, *memoryhistory.Selection, VolumeNodeIdentity) error) (memoryhistory.Resolver, error) {
	if config == nil || config.Insecure || !strings.HasPrefix(config.Host, "https://") || authorize == nil || nodeIdentity == nil {
		return nil, memoryhistory.ErrInvalid
	}
	reader, err := newVolumeHealthReader(config, VolumeHealthOptions{Namespace: "default", Timeout: 5 * time.Second})
	if err != nil {
		return nil, memoryhistory.ErrUnavailable
	}
	return &memoryHistoryResolver{reader: reader, authorize: authorize, nodeIdentity: nodeIdentity, calls: rate.NewLimiter(20, 40), gate: make(chan struct{}, 1), subresources: subresources, workload: workload}, nil
}

func (r *memoryHistoryResolver) Resolve(ctx context.Context, request memoryhistory.Request) (memoryhistory.Selection, error) {
	if err := request.Validate(); err != nil {
		return memoryhistory.Selection{}, err
	}
	select {
	case r.gate <- struct{}{}:
		defer func() { <-r.gate }()
	default:
		return memoryhistory.Selection{}, memoryhistory.ErrBounds
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	q := volumeBindingQuery{healthQuery: healthQuery{reader: r.reader, remaining: 4 << 20, validateJSON: boundedVolumeObject, beforeRequest: r.calls.Wait}, workloadParents: map[string]workloadObject{}}
	q.authorize = func(ctx context.Context, a ObjectAccess) error {
		if err := r.calls.Wait(ctx); err != nil {
			return err
		}
		return r.authorize(ctx, a)
	}
	resource := "pods"
	switch request.Scope {
	case memoryhistory.Node:
		resource = "nodes"
	case memoryhistory.Workload:
		resource = "workloads"
	}
	// Resolve runs before acquisition and again before disclosure. Recheck the
	// named history permission even when the caller keeps ordinary object reads.
	for _, subresource := range r.subresources {
		if err := q.authorize(ctx, ObjectAccess{Group: api.MemoryAPIGroup, Resource: resource, Subresource: subresource, Namespace: request.Namespace, Name: request.Name}); err != nil {
			return memoryhistory.Selection{}, historyReadError(err)
		}
	}
	s := memoryhistory.Selection{Request: request, ResolvedAt: time.Now().UTC()}
	var err error
	switch request.Scope {
	case memoryhistory.Node:
		err = q.historyNode(ctx, &s)
	case memoryhistory.Pod, memoryhistory.Container:
		var pod corev1.Pod
		pod, err = q.historyPod(ctx, request.Namespace, request.Name)
		if err == nil {
			s.UID = string(pod.UID)
			err = q.historyTargets(ctx, &s, pod, r.nodeIdentity)
		}
	case memoryhistory.Workload:
		err = r.workload(&q, ctx, &s, r.nodeIdentity)
	}
	if err != nil {
		return memoryhistory.Selection{}, historyReadError(err)
	}
	if ctx.Err() != nil {
		return memoryhistory.Selection{}, ctx.Err()
	}
	s.ResolvedAt = time.Now().UTC()
	slices.SortFunc(s.Targets, func(a, b memoryhistory.Target) int {
		if n := strings.Compare(a.Pod, b.Pod); n != 0 {
			return n
		}
		return strings.Compare(a.Container, b.Container)
	})
	if err := s.Validate(); err != nil {
		return memoryhistory.Selection{}, err
	}
	return s, nil
}

func historyReadError(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	for _, known := range []error{memoryhistory.ErrInvalid, memoryhistory.ErrBounds, memoryhistory.ErrDenied, memoryhistory.ErrChanged, memoryhistory.ErrNotFound, memoryhistory.ErrUnavailable, changemarkers.ErrUnsupported} {
		if errors.Is(err, known) {
			return known
		}
	}
	if errors.Is(err, ErrVolumeWorkloadBounds) {
		return memoryhistory.ErrBounds
	}
	if errors.Is(err, errHealthObjectNotFound) {
		return memoryhistory.ErrNotFound
	}
	var failure *HealthReadError
	if errors.As(err, &failure) {
		switch failure.Reason {
		case volumehealth.AccessDenied:
			return memoryhistory.ErrDenied
		case volumehealth.InvalidResponse:
			return memoryhistory.ErrInvalid
		}
	}
	return memoryhistory.ErrUnavailable
}
