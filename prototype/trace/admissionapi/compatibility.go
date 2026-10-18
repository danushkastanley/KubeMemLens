package admissionapi

import (
	"net/http"
	"strings"

	admission "github.com/danushkastanley/kube-memlens/internal/traceadmission"
	"github.com/danushkastanley/kube-memlens/internal/tracecompat"
)

func (h *Handler) negotiate(r *http.Request) (tracecompat.Version, error) {
	// Cancellation keeps its existing stable status response and authorisation.
	// An incompatible reader must still be able to release its owned resources.
	if r.Method == http.MethodDelete {
		return tracecompat.Legacy, nil
	}
	selected, err := tracecompat.Negotiate(r.Header.Values(tracecompat.Header), tracecompat.Range{Min: tracecompat.Minimum, Max: tracecompat.Maximum})
	if err != nil {
		return tracecompat.Legacy, err
	}
	if selected == tracecompat.Current && h.stream != nil {
		for kind, programme := range h.stream.programmes {
			if !tracecompat.StreamCompatible(kind, programme.Version) {
				return tracecompat.Legacy, tracecompat.ErrIncompatible
			}
		}
	}
	return selected, nil
}

func contractQuery(r *http.Request, version tracecompat.Version) bool {
	stream := r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/stream")
	if version == tracecompat.Current && stream {
		// Older handlers reject every query before claiming a lease. This prevents
		// a rolling downgrade from ignoring a header and activating an unreadable trace.
		return r.URL.RawQuery == tracecompat.StreamQuery
	}
	return r.URL.RawQuery == ""
}

func contractRequest(version tracecompat.Version, r admission.Request) error {
	switch version {
	case tracecompat.Legacy:
		if r.SchemaVersion() != tracecompat.RequestSchema {
			return nil
		}
	case tracecompat.Current:
		if r.SchemaVersion() == tracecompat.RequestSchema {
			return nil
		}
	}
	return tracecompat.ErrIncompatible
}
