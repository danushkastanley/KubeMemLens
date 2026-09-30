package admissionapi

import (
	"net/http"
	"time"

	admission "github.com/danushkastanley/kube-memlens/internal/traceadmission"
	"k8s.io/apiserver/pkg/authentication/user"
)

func (h *Handler) servePreflight(w http.ResponseWriter, r *http.Request, principal user.Info, namespace string) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if r.Header.Get("Content-Type") != "application/json" {
		writeError(w, admission.ErrInvalidRequest)
		return
	}
	intent, err := admission.DecodeRequest(namespace, http.MaxBytesReader(w, r.Body, admission.MaxRequestBytes))
	if err != nil {
		writeError(w, err)
		return
	}
	result, err := h.manager.Preflight(r.Context(), principal, intent)
	if err != nil {
		writeError(w, err)
		return
	}
	if h.stream == nil {
		writeError(w, admission.ErrUnavailable)
		return
	}
	programme, approved := h.stream.programmes[intent.Kind()]
	if !approved || result.ProgrammeDigest != programme.Digest || result.StreamVersion != programme.Version {
		writeError(w, admission.ErrUnavailable)
		return
	}
	document, err := admission.NewPreflightDocument(intent, result, time.Now())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, document)
}
