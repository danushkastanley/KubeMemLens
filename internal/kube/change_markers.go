package kube

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/changemarkers"
	"github.com/danushkastanley/kube-memlens/internal/memoryhistory"
	"golang.org/x/time/rate"
	"k8s.io/client-go/rest"
)

type changeMarkerProvider struct {
	reader    *volumeHealthReader
	authorize ObjectAuthorizer
	calls     *rate.Limiter
	gate      chan struct{}
	now       func() time.Time
}

func NewChangeMarkerProvider(config *rest.Config, authorize ObjectAuthorizer) (changemarkers.Provider, error) {
	if config == nil || config.Insecure || !strings.HasPrefix(config.Host, "https://") || authorize == nil {
		return nil, memoryhistory.ErrInvalid
	}
	reader, err := newVolumeHealthReader(config, VolumeHealthOptions{Namespace: "default", Timeout: 2 * time.Second})
	if err != nil {
		return nil, memoryhistory.ErrUnavailable
	}
	return &changeMarkerProvider{reader: reader, authorize: authorize, calls: rate.NewLimiter(40, 80), gate: make(chan struct{}, 1), now: time.Now}, nil
}

func (p *changeMarkerProvider) query() *markerQuery {
	q := &markerQuery{objects: map[string]markerObject{}}
	q.volumeBindingQuery = volumeBindingQuery{healthQuery: healthQuery{reader: p.reader, remaining: 4 << 20, validateJSON: boundedVolumeObject, beforeRequest: p.calls.Wait}, authorize: func(ctx context.Context, a ObjectAccess) error {
		if err := p.calls.Wait(ctx); err != nil {
			return err
		}
		return p.authorize(ctx, a)
	}}
	return q
}

func (p *changeMarkerProvider) Query(ctx context.Context, selected memoryhistory.Selection, query memoryhistory.Query) (changemarkers.Report, error) {
	if selected.Validate() != nil || query.Validate(p.now()) != nil || selected.Request.Scope == memoryhistory.Node {
		return changemarkers.Report{}, memoryhistory.ErrInvalid
	}
	select {
	case p.gate <- struct{}{}:
		defer func() { <-p.gate }()
	default:
		return changemarkers.Report{}, memoryhistory.ErrBounds
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	q := p.query()
	if err := q.authoriseSelection(ctx, selected.Request); err != nil {
		return changemarkers.Report{}, historyReadError(err)
	}
	if err := q.preparePods(ctx, selected.Request, selected.UID); err != nil {
		return changemarkers.Report{}, historyReadError(err)
	}
	candidates, err := q.selectedMarkers(ctx, selected, p.now)
	if errors.Is(err, changemarkers.ErrUnsupported) {
		return changemarkers.Report{}, err
	}
	if err != nil {
		return changemarkers.Report{}, historyReadError(err)
	}
	events, coverage, truncated, err := q.events(ctx, selected)
	if err != nil {
		return changemarkers.Report{}, historyReadError(err)
	}
	candidates = append(candidates, events...)
	observed := p.now().UTC()
	// Revision is context at this observation, not at an old object's creation.
	for i := range candidates {
		if candidates[i].Kind == changemarkers.Revision {
			candidates[i].At = observed
			candidates[i].Until = observed
		}
	}
	result, err := changemarkers.Compose(selected, query, observed, coverage, candidates, truncated)
	if errors.Is(err, changemarkers.ErrBounds) {
		return changemarkers.Report{}, memoryhistory.ErrBounds
	}
	if err != nil {
		return changemarkers.Report{}, memoryhistory.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return changemarkers.Report{}, err
	}
	return result, nil
}

func (p *changeMarkerProvider) Revalidate(ctx context.Context, report changemarkers.Report) error {
	if report.Validate() != nil {
		return memoryhistory.ErrInvalid
	}
	select {
	case p.gate <- struct{}{}:
		defer func() { <-p.gate }()
	default:
		return memoryhistory.ErrBounds
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	q := p.query()
	if err := q.authoriseSelection(ctx, report.Request); err != nil {
		return historyReadError(err)
	}
	if err := q.preparePods(ctx, report.Request, report.UID); err != nil {
		return historyReadError(err)
	}
	if report.Events != changemarkers.Denied && report.Events != changemarkers.Disabled {
		if err := q.authorize(ctx, eventAccess(report.Request.Namespace)); err != nil {
			return historyReadError(err)
		}
	}
	for _, marker := range report.Markers {
		current, err := q.object(ctx, marker.Subject)
		if err != nil {
			return historyReadError(err)
		}
		for _, owner := range marker.Owners {
			if !sameMarkerOwner(current.controller, owner) {
				return memoryhistory.ErrChanged
			}
			current, err = q.object(ctx, owner)
			if err != nil {
				return historyReadError(err)
			}
		}
		if current.controller != nil {
			return memoryhistory.ErrChanged
		}
	}
	return ctx.Err()
}

func (q *markerQuery) authoriseSelection(ctx context.Context, request memoryhistory.Request) error {
	resource := "pods"
	if request.Scope == memoryhistory.Workload {
		resource = "workloads"
	}
	for _, subresource := range []string{"trends-context", "trends"} {
		if err := q.authorize(ctx, ObjectAccess{Group: api.MemoryAPIGroup, Resource: resource, Subresource: subresource, Namespace: request.Namespace, Name: request.Name}); err != nil {
			return err
		}
	}
	return nil
}

func eventAccess(namespace string) ObjectAccess {
	return ObjectAccess{Resource: "events", Namespace: namespace, Verb: "list"}
}

func markerDenied(err error) bool { return errors.Is(historyReadError(err), memoryhistory.ErrDenied) }
