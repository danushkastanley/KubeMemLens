package extension

import (
	"context"
	"encoding/json"
	"errors"
	"runtime"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/buildinfo"
	"github.com/danushkastanley/kube-memlens/internal/changemarkers"
	"github.com/danushkastanley/kube-memlens/internal/incidentsession"
	"github.com/danushkastanley/kube-memlens/internal/memoryhistory"
)

func (s *sessionCaptureSource) CaptureMarkers(ctx context.Context, p incidentsession.Principal, pod string, query memoryhistory.Query) ([]byte, error) {
	request := memoryhistory.Request{Scope: memoryhistory.Pod, Namespace: p.Namespace, Name: pod}
	if request.Validate() != nil || query.Validate(s.reads.now()) != nil {
		return nil, incidentsession.ErrInvalid
	}
	history := s.reads.memoryHistory
	if history == nil || !history.namespaces[p.Namespace] || history.contextResolver == nil || history.markers == nil {
		return nil, incidentsession.ErrDisabled
	}
	select {
	case history.gate <- struct{}{}:
		defer func() { <-history.gate }()
	default:
		return nil, incidentsession.ErrCapacity
	}
	evidence, err := history.acquire(ctx, request, query, history.contextResolver, history.markers)
	if err != nil {
		return nil, sessionHistoryError(err)
	}
	bundle := api.HistoryIncident{SchemaVersion: api.HistoryIncidentSchemaVersion, CapturedAt: s.reads.now().UTC(), ToolVersion: buildinfo.Current(runtime.Version(), runtime.GOOS, runtime.GOARCH).String(),
		Context: changemarkers.Context{SchemaVersion: 1, History: evidence.History, Changes: evidence.Changes}, Caveats: api.HistoryIncidentCaveats()}
	if api.ValidateHistoryIncident(bundle) != nil {
		return nil, incidentsession.ErrUnavailable
	}
	data, err := json.Marshal(bundle)
	if err != nil {
		return nil, incidentsession.ErrUnavailable
	}
	if len(data) > incidentsession.MaxCaptureBytes {
		return nil, incidentsession.ErrCapacity
	}
	return data, nil
}

func sessionHistoryError(err error) error {
	switch {
	case errors.Is(err, changemarkers.ErrUnsupported):
		return incidentsession.ErrUnsupported
	case errors.Is(err, memoryhistory.ErrDenied):
		return incidentsession.ErrDenied
	case errors.Is(err, memoryhistory.ErrChanged):
		return incidentsession.ErrChanged
	case errors.Is(err, memoryhistory.ErrBounds), errors.Is(err, changemarkers.ErrBounds):
		return incidentsession.ErrCapacity
	default:
		return incidentsession.ErrUnavailable
	}
}
