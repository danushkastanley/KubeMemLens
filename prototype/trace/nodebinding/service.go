package nodebinding

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	admission "github.com/danushkastanley/kube-memlens/internal/traceadmission"
	"github.com/danushkastanley/kube-memlens/internal/tracepreflight"
	"github.com/danushkastanley/kube-memlens/prototype/trace/targetfs"
)

type Resolve func(context.Context, admission.Workload) (targetfs.Handle, error)
type Preflight func(context.Context) (string, error)

type lease struct {
	owner         string
	expires       time.Time
	handle        targetfs.Handle
	specification trace.Specification
	execution     *execution
}

type Service struct {
	mu                sync.Mutex
	nodeUID, nodeName string
	instance          string
	control           Peer
	resolve           Resolve
	preflight         Preflight
	audit             func(string)
	leases            map[string]*lease
	seen              map[string]time.Time
	slots             chan struct{}
	streamSlots       chan struct{}
	runtime           Runtime
	ctx               context.Context
	cancel            context.CancelFunc
	done              chan struct{}
	closed            bool
	cleanupErr        error
}

// NewService owns at most two handles and 256 unexpired replay records. When
// replay storage is full it refuses work; it never evicts a live nonce.
func NewService(ctx context.Context, uid, name string, control Peer, resolve Resolve, preflight Preflight, audit func(string), runtime Runtime) (*Service, error) {
	if ctx == nil || ctx.Err() != nil || uid == "" || name == "" || !control.valid() || resolve == nil || preflight == nil || audit == nil {
		return nil, admission.ErrUnavailable
	}
	instance, err := newInstance()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(ctx)
	s := &Service{instance: instance, nodeUID: uid, nodeName: name, control: control, resolve: resolve, preflight: preflight, audit: audit, leases: map[string]*lease{}, seen: map[string]time.Time{}, slots: make(chan struct{}, 2), streamSlots: make(chan struct{}, 2), runtime: runtime, ctx: ctx, cancel: cancel, done: make(chan struct{})}
	go s.expire(ctx)
	return s, nil
}

func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.TLS == nil || s.control.verify(*r.TLS) != nil {
		http.Error(w, "unauthorised", http.StatusUnauthorized)
		return
	}
	if r.URL.RawQuery != "" || r.URL.RawPath != "" {
		http.Error(w, "invalid request", 400)
		return
	}
	owner := r.Header.Get(controllerHeader)
	if !validID(owner) {
		writeError(w, admission.ErrDenied)
		return
	}
	if r.Method == http.MethodGet && r.URL.Path == "/v1/identity" {
		s.writeIdentity(w)
		return
	}
	if r.Header.Get(nodeHeader) != s.instance {
		writeError(w, admission.ErrUnavailable)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/v1/bindings/") {
		id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/v1/bindings/"), "/stream")
		s.mu.Lock()
		existing := s.leases[id]
		denied := existing != nil && existing.owner != owner
		s.mu.Unlock()
		if denied {
			writeError(w, admission.ErrDenied)
			return
		}
	}
	if id, ok := streamID(r); ok {
		s.serveStream(w, r, id)
		return
	}
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	default:
		http.Error(w, "capacity", http.StatusTooManyRequests)
		return
	}
	if r.URL.RawQuery != "" || r.URL.RawPath != "" {
		http.Error(w, "invalid request", 400)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 4*time.Second)
	defer cancel()
	if r.Method == http.MethodPost && r.URL.Path == "/v1/bindings" {
		var request bindRequest
		if r.Header.Get("Content-Type") != "application/json" || decode(http.MaxBytesReader(w, r.Body, maxBody), &request, "id|expires|namespace|pod|podUID|container|containerID|started|nodeUID|nodeName|qos|kind|paths|durationNanos|events|outputBytes|mapBytes|pathBytes") != nil {
			http.Error(w, "invalid request", 400)
			return
		}
		request.controller = owner
		response, err := s.bind(ctx, request)
		if err != nil {
			writeError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/v1/bindings/")
	if !strings.HasPrefix(r.URL.Path, "/v1/bindings/") || !validID(id) {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", 405)
		return
	}
	if err := s.operation(ctx, id, r.Method == http.MethodDelete); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) bind(ctx context.Context, r bindRequest) (bindResponse, error) {
	w := r.workload()
	remaining := time.Until(r.Expires)
	if trace.ValidateIntent(r.Kind, r.Paths, r.bounds()) != nil || !validID(r.ID) || remaining <= 0 || remaining > maxLease || w.Target.ValidateLifetime() != nil || r.NodeUID != s.nodeUID || r.NodeName != s.nodeName {
		return bindResponse{}, admission.ErrTargetChanged
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prune(time.Now())
	if s.closed || s.cleanupErr != nil || ctx.Err() != nil {
		return bindResponse{}, admission.ErrUnavailable
	}
	if _, exists := s.seen[r.ID]; exists {
		return bindResponse{}, admission.ErrExpired
	}
	if len(s.leases) >= 2 || len(s.seen) >= 256 {
		return bindResponse{}, admission.ErrCapacity
	}
	s.seen[r.ID] = r.Expires
	// The preflight adapter must report an observed successful baseline; merely
	// naming a profile is not a substitute for executing it in the node runtime.
	profile, err := s.preflight(ctx)
	if err != nil || profile != tracepreflight.Baseline().Digest() {
		return bindResponse{}, admission.ErrUnavailable
	}
	if ctx.Err() != nil || !time.Now().Before(r.Expires) {
		return bindResponse{}, admission.ErrExpired
	}
	handle, err := s.resolve(ctx, w)
	if err != nil {
		return bindResponse{}, admission.ErrTargetChanged
	}
	if handle == nil {
		return bindResponse{}, admission.ErrUnavailable
	}
	target := handle.Target()
	expected := w.Target
	expected.CgroupID = target.CgroupID
	if target != expected || target.CgroupID == 0 || ctx.Err() != nil || !time.Now().Before(r.Expires) {
		if s.closeHandle(handle) != nil {
			return bindResponse{}, admission.ErrUnavailable
		}
		return bindResponse{}, admission.ErrTargetChanged
	}
	specification, err := trace.NewSpecification(r.Kind, target, r.Paths, r.bounds())
	if err != nil {
		if s.closeHandle(handle) != nil {
			return bindResponse{}, admission.ErrUnavailable
		}
		return bindResponse{}, admission.ErrTargetChanged
	}
	s.leases[r.ID] = &lease{owner: r.controller, expires: r.Expires, handle: handle, specification: specification}
	return bindResponse{target.CgroupID, profile}, nil
}

func (s *Service) closeHandle(h targetfs.Handle) error {
	if err := h.Close(); err != nil {
		s.cleanupErr = admission.ErrUnavailable
		s.audit("cleanup_unconfirmed")
		return admission.ErrUnavailable
	}
	return nil
}

func writeError(w http.ResponseWriter, err error) {
	code := http.StatusServiceUnavailable
	switch err {
	case admission.ErrDenied:
		code = 403
	case admission.ErrCapacity:
		code = 429
	case admission.ErrExpired:
		code = 410
	case admission.ErrTargetChanged:
		code = 409
	}
	http.Error(w, http.StatusText(code), code)
}
