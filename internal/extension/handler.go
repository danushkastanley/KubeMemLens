package extension

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"github.com/danushkastanley/kube-memlens/internal/api"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apiserver/pkg/authentication/user"
	apirequest "k8s.io/apiserver/pkg/endpoints/request"
)

const (
	PodUIDExtra       = "authentication.kubernetes.io/pod-uid"
	NodeNameExtra     = "authentication.kubernetes.io/node-name"
	NodeUIDExtra      = "authentication.kubernetes.io/node-uid"
	CredentialIDExtra = "authentication.kubernetes.io/credential-id"
)

type HandlerOptions struct {
	AgentUsername    string
	MaxSnapshotBytes int64
	MaxConcurrent    int
	RequestsPerSec   float64
	Burst            int
	MaxIdentities    int
	IdentityTTL      time.Duration
	Logf             func(string, ...any)
}

type Handler struct {
	coordinator *Coordinator
	opts        HandlerOptions
	concurrent  chan struct{}
	limiterMu   sync.Mutex
	limiters    map[string]identityLimiter
}

type identityLimiter struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

func NewHandler(coordinator *Coordinator, opts HandlerOptions) (*Handler, error) {
	if coordinator == nil {
		return nil, fmt.Errorf("ingestion coordinator is required")
	}
	if strings.TrimSpace(opts.AgentUsername) == "" {
		return nil, fmt.Errorf("agent username is required")
	}
	if opts.MaxSnapshotBytes <= 0 || opts.MaxConcurrent <= 0 || opts.RequestsPerSec <= 0 || opts.Burst <= 0 || opts.MaxIdentities <= 0 {
		return nil, fmt.Errorf("ingestion request limits must be greater than zero")
	}
	if opts.IdentityTTL <= 0 {
		opts.IdentityTTL = 2 * time.Hour
	}
	if opts.Logf == nil {
		opts.Logf = func(string, ...any) {}
	}
	return &Handler{
		coordinator: coordinator,
		opts:        opts,
		concurrent:  make(chan struct{}, opts.MaxConcurrent),
		limiters:    map[string]identityLimiter{},
	}, nil
}

type routeMux interface {
	HandleFunc(string, func(http.ResponseWriter, *http.Request))
}

func (h *Handler) Register(mux routeMux) {
	groupVersion := "/apis/" + api.MemoryAPIGroup + "/" + api.MemoryAPIVersion
	mux.HandleFunc(groupVersion, h.discovery)
	mux.HandleFunc(groupVersion+"/ingestionepochs/current", h.epoch)
	mux.HandleFunc(groupVersion+"/nodesnapshots", h.snapshot)
}

func (h *Handler) discovery(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	writeJSON(w, http.StatusOK, metav1.APIResourceList{
		TypeMeta:     metav1.TypeMeta{APIVersion: "v1", Kind: "APIResourceList"},
		GroupVersion: api.MemoryAPIGroup + "/" + api.MemoryAPIVersion,
		APIResources: []metav1.APIResource{
			{Name: "ingestionepochs", Namespaced: false, Kind: "IngestionEpoch", Verbs: metav1.Verbs{"get"}},
			{Name: "nodesnapshots", Namespaced: false, Kind: "NodeSnapshot", Verbs: metav1.Verbs{"create"}},
		},
	})
}

func (h *Handler) epoch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	claims, err := agentClaims(r, h.opts.AgentUsername)
	if err != nil {
		writeAPIError(w, http.StatusForbidden, "agent_identity", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, h.coordinator.Epoch(claims.PodUID))
}

func (h *Handler) snapshot(w http.ResponseWriter, r *http.Request) {
	startedAt := time.Now()
	result := "identity_rejected"
	nodeClaimMatch := "unknown"
	tracked := &statusWriter{ResponseWriter: w, status: http.StatusOK}
	w = tracked
	defer func() {
		h.coordinator.store.RecordIngestion(result, time.Since(startedAt))
		h.opts.Logf(
			"security request_id=%s principal=agent verb=create resource=nodesnapshots scope=cluster decision=%s reason=%s node_claim_match=%s status=%d duration=%s",
			requestID(r), ingestionDecision(result), result, nodeClaimMatch, tracked.status, time.Since(startedAt).Round(time.Millisecond),
		)
	}()
	if r.Method != http.MethodPost {
		result = "method_not_allowed"
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	claims, err := agentClaims(r, h.opts.AgentUsername)
	if err != nil {
		writeAPIError(w, http.StatusForbidden, "agent_identity", err.Error())
		return
	}
	if encoding := strings.TrimSpace(r.Header.Get("Content-Encoding")); encoding != "" && !strings.EqualFold(encoding, "identity") {
		result = "content_encoding_rejected"
		writeAPIError(w, http.StatusUnsupportedMediaType, "content_encoding", "compressed request bodies are not accepted")
		return
	}
	mediaType, _, mediaErr := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if mediaErr != nil || mediaType != "application/json" {
		result = "unsupported_media_type"
		writeAPIError(w, http.StatusUnsupportedMediaType, "content_type", "Content-Type must be application/json")
		return
	}
	if r.ContentLength > h.opts.MaxSnapshotBytes {
		result = "payload_too_large"
		writeAPIError(w, http.StatusRequestEntityTooLarge, "payload_too_large", "snapshot exceeds maximum request size")
		return
	}
	if !h.allowIdentity(claims.NodeUID, time.Now()) {
		result = "rate_limited"
		w.Header().Set("Retry-After", "1")
		writeAPIError(w, http.StatusTooManyRequests, "rate_limited", "ingestion rate limit exceeded")
		return
	}
	select {
	case h.concurrent <- struct{}{}:
		defer func() { <-h.concurrent }()
	default:
		result = "concurrency_limited"
		w.Header().Set("Retry-After", "1")
		writeAPIError(w, http.StatusTooManyRequests, "concurrency_limited", "ingestion concurrency limit exceeded")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, h.opts.MaxSnapshotBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		var limitErr *http.MaxBytesError
		if errors.As(err, &limitErr) {
			result = "payload_too_large"
			writeAPIError(w, http.StatusRequestEntityTooLarge, "payload_too_large", "snapshot exceeds maximum request size")
			return
		}
		result = "body_error"
		writeAPIError(w, http.StatusBadRequest, "body_error", "could not read snapshot body")
		return
	}
	var request api.NodeSnapshotRequest
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		result = "invalid_json"
		writeAPIError(w, http.StatusBadRequest, "invalid_json", "invalid snapshot JSON")
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		result = "invalid_json"
		writeAPIError(w, http.StatusBadRequest, "invalid_json", "snapshot must contain one JSON object")
		return
	}
	response, duplicate, err := h.coordinator.Accept(claims, request)
	if request.Snapshot.NodeName == claims.NodeName && request.NodeUID == claims.NodeUID {
		nodeClaimMatch = "true"
	} else {
		nodeClaimMatch = "false"
	}
	if err != nil {
		var ingestionErr *IngestionError
		if errors.As(err, &ingestionErr) {
			result = ingestionErr.Result
			writeAPIError(w, ingestionErr.Status, ingestionErr.Code, ingestionErr.Message)
			return
		}
		result = "store_error"
		writeAPIError(w, http.StatusInternalServerError, "store_error", "store snapshot")
		return
	}
	if duplicate {
		result = "duplicate"
	} else {
		result = "accepted"
	}
	writeJSON(w, http.StatusOK, response)
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func ingestionDecision(result string) string {
	switch result {
	case "accepted", "duplicate":
		return "allow"
	case "store_error":
		return "error"
	default:
		return "deny"
	}
}

func requestID(request *http.Request) string {
	value := request.Header.Get("Audit-ID")
	if validRequestID(value) {
		return value
	}
	var generated [16]byte
	if _, err := rand.Read(generated[:]); err != nil {
		return "unavailable"
	}
	return hex.EncodeToString(generated[:])
}

func validRequestID(value string) bool {
	if value == "" || len(value) > 64 {
		return false
	}
	for _, character := range value {
		if character != '-' && character != '_' && (character < '0' || character > '9') &&
			(character < 'A' || character > 'Z') && (character < 'a' || character > 'z') {
			return false
		}
	}
	return true
}

func (h *Handler) allowIdentity(nodeUID string, now time.Time) bool {
	h.limiterMu.Lock()
	defer h.limiterMu.Unlock()
	entry, exists := h.limiters[nodeUID]
	if !exists {
		cutoff := now.Add(-h.opts.IdentityTTL)
		for key, candidate := range h.limiters {
			if candidate.lastSeen.Before(cutoff) {
				delete(h.limiters, key)
			}
		}
		if len(h.limiters) >= h.opts.MaxIdentities {
			oldestKey := ""
			oldestSeen := now
			for key, candidate := range h.limiters {
				if oldestKey == "" || candidate.lastSeen.Before(oldestSeen) {
					oldestKey = key
					oldestSeen = candidate.lastSeen
				}
			}
			delete(h.limiters, oldestKey)
		}
		entry = identityLimiter{limiter: rate.NewLimiter(rate.Limit(h.opts.RequestsPerSec), h.opts.Burst)}
	}
	entry.lastSeen = now
	h.limiters[nodeUID] = entry
	return entry.limiter.Allow()
}

func agentClaims(r *http.Request, expectedUsername string) (AgentClaims, error) {
	user, ok := apirequest.UserFrom(r.Context())
	if !ok {
		return AgentClaims{}, fmt.Errorf("request is not from the configured agent ServiceAccount")
	}
	return claimsFromUser(user, expectedUsername)
}

func claimsFromUser(info user.Info, expectedUsername string) (AgentClaims, error) {
	if info == nil || info.GetName() != expectedUsername {
		return AgentClaims{}, fmt.Errorf("request is not from the configured agent ServiceAccount")
	}
	extras := info.GetExtra()
	podUID, err := oneExtra(extras, PodUIDExtra)
	if err != nil {
		return AgentClaims{}, err
	}
	nodeName, err := oneExtra(extras, NodeNameExtra)
	if err != nil {
		return AgentClaims{}, err
	}
	nodeUID, err := oneExtra(extras, NodeUIDExtra)
	if err != nil {
		return AgentClaims{}, err
	}
	credentialID, err := oneExtra(extras, CredentialIDExtra)
	if err != nil {
		return AgentClaims{}, err
	}
	return AgentClaims{PodUID: podUID, NodeName: nodeName, NodeUID: nodeUID, CredentialID: credentialID}, nil
}

func oneExtra(extras map[string][]string, name string) (string, error) {
	values := extras[name]
	if len(values) != 1 || strings.TrimSpace(values[0]) == "" {
		return "", fmt.Errorf("authenticated identity is missing one required claim")
	}
	return values[0], nil
}

func writeAPIError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, metav1.Status{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Status"},
		Status:   metav1.StatusFailure, Reason: metav1.StatusReason(code), Message: message, Code: int32(status),
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
