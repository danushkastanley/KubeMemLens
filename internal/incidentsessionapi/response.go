package incidentsessionapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/incidentsession"
	"github.com/danushkastanley/kube-memlens/internal/kubeprincipal"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type SessionResource = incidentsession.Resource

func writeSummary(w http.ResponseWriter, status int, namespace string, summary incidentsession.Summary) {
	value := SessionResource{APIVersion: api.MemoryAPIGroup + "/" + api.MemoryAPIVersion, Kind: "IncidentSession", SchemaVersion: incidentsession.SchemaVersion, Session: summary}
	value.Metadata.Name = summary.ID
	value.Metadata.Namespace = namespace
	writeValue(w, status, value)
}

func writeError(w http.ResponseWriter, err error) {
	status, reason, message := http.StatusServiceUnavailable, metav1.StatusReasonServiceUnavailable, "incident session service unavailable"
	switch {
	case errors.Is(err, kubeprincipal.ErrUnauthenticated):
		status, reason, message = http.StatusUnauthorized, metav1.StatusReasonUnauthorized, "authentication is required"
	case errors.Is(err, incidentsession.ErrDenied):
		status, reason, message = http.StatusForbidden, metav1.StatusReasonForbidden, "incident session access denied"
	case errors.Is(err, incidentsession.ErrInvalid):
		status, reason, message = http.StatusBadRequest, metav1.StatusReasonBadRequest, "invalid incident session request or schema"
	case errors.Is(err, incidentsession.ErrNotFound):
		status, reason, message = http.StatusNotFound, metav1.StatusReasonNotFound, "incident session unavailable"
	case errors.Is(err, incidentsession.ErrClosed):
		status, reason, message = http.StatusConflict, metav1.StatusReasonConflict, "incident session is closed"
	case errors.Is(err, incidentsession.ErrCapacity):
		status, reason, message = http.StatusTooManyRequests, metav1.StatusReasonTooManyRequests, "incident session capacity reached"
	}
	writeValue(w, status, metav1.Status{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Status"}, Status: metav1.StatusFailure, Reason: reason, Code: int32(status), Message: message})
}

func writeValue(w http.ResponseWriter, status int, value any) {
	data, err := json.Marshal(value)
	if err != nil {
		panic(http.ErrAbortHandler)
	}
	writeBytes(w, status, data)
}

func writeBytes(w http.ResponseWriter, status int, data []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if n, err := w.Write(data); err != nil || n != len(data) {
		panic(http.ErrAbortHandler)
	}
}
