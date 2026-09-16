// Package admissionapi exposes the optional namespace-scoped admission API.
package admissionapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	admission "github.com/danushkastanley/kube-memlens/internal/traceadmission"
	"golang.org/x/time/rate"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apiserver/pkg/endpoints/request"
)

const groupVersion = admission.APIGroup + "/" + admission.APIVersion
const prefix = "/apis/" + groupVersion

type Handler struct {
	manager     *admission.Manager
	rate        *rate.Limiter
	stream      *StreamProxy
	streamSlots chan struct{}
}

func NewHandler(manager *admission.Manager) *Handler {
	return &Handler{manager: manager, rate: rate.NewLimiter(20, 20), streamSlots: make(chan struct{}, 32)}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if h.manager == nil {
		writeError(w, admission.ErrUnavailable)
		return
	}
	principal, ok := request.UserFrom(r.Context())
	if !ok || principal == nil {
		writeError(w, admission.ErrUnauthenticated)
		return
	}
	if !h.rate.Allow() {
		writeError(w, admission.ErrCapacity)
		return
	}
	// Namespace deletion probes list/deletecollection even for resources added
	// after the controller's discovery cache was populated. These admissions
	// are ephemeral and have no collection API. Report that truthfully rather
	// than returning a bad request that blocks namespace finalisation.
	collection := strings.Split(strings.TrimPrefix(r.URL.Path, prefix+"/namespaces/"), "/")
	if r.URL.RawPath == "" && strings.HasPrefix(r.URL.Path, prefix+"/namespaces/") && len(collection) == 2 && collection[0] != "" && collection[1] == "traces" && (r.Method == http.MethodGet || r.Method == http.MethodDelete) {
		w.Header().Set("Allow", http.MethodPost)
		writeJSON(w, http.StatusMethodNotAllowed, metav1.Status{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Status"}, Status: metav1.StatusFailure, Code: http.StatusMethodNotAllowed, Reason: metav1.StatusReasonMethodNotAllowed, Message: http.StatusText(http.StatusMethodNotAllowed)})
		return
	}
	if r.URL.RawQuery != "" || r.URL.RawPath != "" {
		writeError(w, admission.ErrInvalidRequest)
		return
	}
	if r.Method == http.MethodGet && r.URL.Path == prefix {
		writeJSON(w, 200, metav1.APIResourceList{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "APIResourceList"}, GroupVersion: groupVersion, APIResources: resources()})
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, prefix+"/namespaces/"), "/")
	if !strings.HasPrefix(r.URL.Path, prefix+"/namespaces/") || len(parts) < 2 || len(parts) > 4 || parts[1] != "traces" {
		writeError(w, admission.ErrNotFound)
		return
	}
	namespace := parts[0]
	if len(parts) == 4 {
		if parts[3] != "stream" {
			writeError(w, admission.ErrNotFound)
			return
		}
		if len(parts[2]) != 32 || strings.Trim(parts[2], "0123456789abcdef") != "" {
			writeError(w, admission.ErrInvalidRequest)
			return
		}
		h.serveStream(w, r, principal, namespace, parts[2])
		return
	}
	if len(parts) == 2 && r.Method == http.MethodPost {
		if r.Header.Get("Content-Type") != "application/json" {
			writeError(w, admission.ErrInvalidRequest)
			return
		}
		intent, err := admission.DecodeRequest(namespace, http.MaxBytesReader(w, r.Body, admission.MaxRequestBytes))
		if err != nil {
			writeError(w, err)
			return
		}
		result, err := h.manager.Admit(r.Context(), principal, intent)
		if err != nil {
			writeError(w, err)
			return
		}
		writeAdmission(w, 201, namespace, result)
		return
	}
	if len(parts) != 3 || len(parts[2]) != 32 || strings.Trim(parts[2], "0123456789abcdef") != "" {
		writeError(w, admission.ErrInvalidRequest)
		return
	}
	switch r.Method {
	case http.MethodGet:
		result, err := h.manager.Get(r.Context(), principal, namespace, parts[2])
		if err != nil {
			writeError(w, err)
			return
		}
		writeAdmission(w, 200, namespace, result)
	case http.MethodDelete:
		if err := h.manager.Cancel(r.Context(), principal, namespace, parts[2]); err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, 200, metav1.Status{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Status"}, Status: metav1.StatusSuccess, Code: 200})
	default:
		writeError(w, admission.ErrInvalidRequest)
	}
}

type response struct {
	APIVersion   string           `json:"apiVersion"`
	Kind         string           `json:"kind"`
	Metadata     responseMetadata `json:"metadata"`
	State        string           `json:"state"`
	ExpiresAt    time.Time        `json:"expiresAt"`
	EngineDigest string           `json:"engineDigest"`
}
type responseMetadata struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}

func writeAdmission(w http.ResponseWriter, code int, namespace string, a admission.Admission) {
	writeJSON(w, code, response{groupVersion, "TraceAdmission", responseMetadata{a.ID(), namespace}, string(a.State()), a.ExpiresAt(), a.EngineDigest()})
}
func resources() []metav1.APIResource {
	return []metav1.APIResource{{Name: "traces", SingularName: "trace", Namespaced: true, Kind: "TraceAdmission", Verbs: metav1.Verbs{"create", "get", "delete"}, Group: admission.APIGroup, Version: admission.APIVersion}, {Name: "traces/stream", Namespaced: true, Kind: "TraceStream", Verbs: metav1.Verbs{"get"}, Group: admission.APIGroup, Version: admission.APIVersion}}
}
func writeJSON(w http.ResponseWriter, code int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(value)
}
func writeError(w http.ResponseWriter, err error) {
	code, reason := 503, metav1.StatusReasonServiceUnavailable
	switch {
	case errors.Is(err, admission.ErrUnauthenticated):
		code, reason = 401, metav1.StatusReasonUnauthorized
	case errors.Is(err, admission.ErrDenied):
		code, reason = 403, metav1.StatusReasonForbidden
	case errors.Is(err, admission.ErrInvalidRequest):
		code, reason = 400, metav1.StatusReasonBadRequest
	case errors.Is(err, admission.ErrNotFound):
		code, reason = 404, metav1.StatusReasonNotFound
	case errors.Is(err, admission.ErrCapacity):
		code, reason = 429, metav1.StatusReasonTooManyRequests
	case errors.Is(err, admission.ErrExpired):
		code, reason = 410, metav1.StatusReasonGone
	case errors.Is(err, admission.ErrTargetChanged):
		code, reason = 409, metav1.StatusReasonConflict
	}
	writeJSON(w, code, metav1.Status{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Status"}, Status: metav1.StatusFailure, Code: int32(code), Reason: reason, Message: http.StatusText(code)})
}
