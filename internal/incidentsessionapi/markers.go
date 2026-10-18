package incidentsessionapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/danushkastanley/kube-memlens/internal/incidentsession"
	"github.com/danushkastanley/kube-memlens/internal/memoryhistory"
	"k8s.io/apimachinery/pkg/util/validation"
)

type MarkerProvider interface {
	CaptureMarkers(context.Context, incidentsession.Principal, string, memoryhistory.Query) ([]byte, error)
}

type EvidenceProvider interface {
	CaptureProvider
	MarkerProvider
}

func (h *Handler) markers(ctx context.Context, p incidentsession.Principal, id string, r *http.Request) (incidentsession.Summary, error) {
	var input struct {
		Pod   string              `json:"pod"`
		Query memoryhistory.Query `json:"query"`
	}
	if readBody(r, &input) != nil || len(validation.IsDNS1123Subdomain(input.Pod)) != 0 {
		return incidentsession.Summary{}, incidentsession.ErrInvalid
	}
	summary, err := h.store.Inspect(ctx, p, id)
	if err != nil {
		return incidentsession.Summary{}, err
	}
	if summary.ClosedAt != nil {
		return incidentsession.Summary{}, incidentsession.ErrClosed
	}
	var data []byte
	if h.captures == nil {
		err = incidentsession.ErrUnavailable
	} else {
		data, err = h.captures.CaptureMarkers(ctx, p, input.Pod, input.Query)
	}
	if errors.Is(err, incidentsession.ErrInvalid) {
		return incidentsession.Summary{}, err
	}
	if err == nil {
		recorded, recordErr := h.store.RecordMarkers(ctx, p, id, data)
		if !errors.Is(recordErr, incidentsession.ErrInvalid) {
			return recorded, recordErr
		}
		err = incidentsession.ErrUnavailable
	}
	return h.store.RecordMarkerGap(ctx, p, id, captureGapReason(err))
}
