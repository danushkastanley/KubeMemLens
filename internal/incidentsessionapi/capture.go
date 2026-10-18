package incidentsessionapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/danushkastanley/kube-memlens/internal/incidentsession"
	"k8s.io/apimachinery/pkg/util/validation"
)

// CaptureProvider acquires evidence under the authenticated context. It must
// check source permissions and live identity before and after acquisition.
type CaptureProvider interface {
	CapturePod(context.Context, incidentsession.Principal, string) ([]byte, error)
}

func (h *Handler) capture(ctx context.Context, p incidentsession.Principal, id string, r *http.Request) (incidentsession.Summary, error) {
	var input struct {
		Pod string `json:"pod"`
	}
	if readBody(r, &input) != nil || len(validation.IsDNS1123Subdomain(input.Pod)) != 0 {
		return incidentsession.Summary{}, incidentsession.ErrInvalid
	}
	// Establish ownership and open state before reading any source evidence.
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
		data, err = h.captures.CapturePod(ctx, p, input.Pod)
	}
	if err == nil {
		recorded, recordErr := h.store.RecordCapture(ctx, p, id, data)
		if !errors.Is(recordErr, incidentsession.ErrInvalid) {
			return recorded, recordErr
		}
		err = incidentsession.ErrUnavailable
	}
	// The gap is a successful timeline mutation with explicit failed acquisition;
	// raw provider errors, Pod names and partial payloads are never retained.
	return h.store.RecordCaptureGap(ctx, p, id, captureGapReason(err))
}

func captureGapReason(err error) string {
	reason := "source-unavailable"
	if errors.Is(err, incidentsession.ErrDisabled) {
		reason = "source-disabled"
	}
	if errors.Is(err, incidentsession.ErrUnsupported) {
		reason = "source-unsupported"
	}
	if errors.Is(err, incidentsession.ErrCapacity) {
		reason = "source-capacity"
	}
	if errors.Is(err, incidentsession.ErrDenied) {
		reason = "source-denied"
	}
	if errors.Is(err, incidentsession.ErrChanged) {
		reason = "source-changed"
	}
	return reason
}
