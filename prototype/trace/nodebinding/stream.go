package nodebinding

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	admission "github.com/danushkastanley/kube-memlens/internal/traceadmission"
	"github.com/danushkastanley/kube-memlens/internal/traceframe"
	"github.com/danushkastanley/kube-memlens/internal/tracesession"
	"github.com/danushkastanley/kube-memlens/prototype/trace/streamhttp"
	"github.com/danushkastanley/kube-memlens/prototype/trace/targetfs"
)

// Runtime is installation-owned programme selection. Prepare must verify its
// approved artifacts and return a fresh adapter without attaching/loading work.
// The handle is borrowed: Prepare must not close it or allocate a child target
// descriptor. The adapter may duplicate it only during Run, after activation.
// The production command currently supplies nil: no incident programme approved.
type Runtime interface {
	Prepare(context.Context, trace.Specification, targetfs.Handle) (Prepared, error)
}

type Prepared struct {
	Engine          *trace.Engine
	EngineDigest    string
	ProgrammeDigest string
	StreamVersion   int
}
type streamRequest struct {
	Deadline time.Time `json:"deadline"`
	StreamIdentity
}

func (s *Service) serveStream(w http.ResponseWriter, r *http.Request, id string) {
	if s.runtime == nil {
		writeError(w, admission.ErrUnavailable)
		return
	}
	select {
	case s.streamSlots <- struct{}{}:
		defer func() { <-s.streamSlots }()
	default:
		writeError(w, admission.ErrCapacity)
		return
	}
	initial, cancel := context.WithTimeout(r.Context(), 4*time.Second)
	var request streamRequest
	if r.Header.Get("Content-Type") != "application/json" || decode(http.MaxBytesReader(w, r.Body, maxBody), &request, "deadline|engineDigest|programmeDigest|streamVersion") != nil || !request.StreamIdentity.valid() {
		cancel()
		http.Error(w, "invalid request", 400)
		return
	}
	s.mu.Lock()
	l := s.leases[id]
	if l == nil {
		s.mu.Unlock()
		cancel()
		writeError(w, admission.ErrExpired)
		return
	}
	spec := l.specification
	handle := l.handle
	s.mu.Unlock()
	prepared, err := s.runtime.Prepare(initial, spec, handle)
	if err != nil || prepared.Engine == nil {
		cancel()
		writeError(w, admission.ErrUnavailable)
		return
	}
	if prepared.EngineDigest != request.EngineDigest || prepared.ProgrammeDigest != request.ProgrammeDigest || prepared.StreamVersion != request.StreamVersion {
		cancel()
		writeError(w, admission.ErrTargetChanged)
		return
	}
	// Validate before claiming the one-use execution or writing headers.
	now := time.Now().UTC()
	if _, err := traceframe.NewMetadataVersion(traceframe.Metadata{SessionID: id, EngineDigest: prepared.EngineDigest, ProgrammeDigest: prepared.ProgrammeDigest, Specification: spec, SessionStartedAt: now, Deadline: request.Deadline}, prepared.StreamVersion); err != nil {
		cancel()
		writeError(w, admission.ErrUnavailable)
		return
	}
	if initial.Err() != nil {
		cancel()
		writeError(w, admission.ErrUnavailable)
		return
	}
	cancel()
	execution, err := s.activate(r.Context(), id, request.Deadline)
	if err != nil {
		writeError(w, err)
		return
	}
	sink, err := streamhttp.NewSink(w)
	if err != nil {
		_ = execution.finish()
		writeError(w, admission.ErrUnavailable)
		return
	}
	session, err := tracesession.NewVersion(traceframe.Metadata{SessionID: id, EngineDigest: prepared.EngineDigest, ProgrammeDigest: prepared.ProgrammeDigest, Specification: spec}, prepared.Engine, sink, func(ctx context.Context) error {
		if err := execution.handle.Check(ctx); err != nil {
			return tracesession.Stop(trace.TargetChanged)
		}
		return nil
	}, prepared.StreamVersion)
	if err != nil {
		_ = execution.finish()
		writeError(w, admission.ErrUnavailable)
		return
	}
	// A panic must not release ownership under an unconfirmed adapter teardown.
	finished := false
	defer func() {
		if !finished {
			s.mu.Lock()
			s.cleanupErr = admission.ErrUnavailable
			s.mu.Unlock()
			execution.cancel(admission.ErrUnavailable)
			s.audit("cleanup_unconfirmed")
		}
	}()
	scope, stopScope := streamContext(execution.ctx, request.Deadline)
	defer stopScope()
	outcome := session.Run(scope)
	if err := execution.finish(); err != nil {
		s.audit("cleanup_unconfirmed")
	}
	finished = true
	if outcome.Err != nil || !outcome.TerminalDelivered {
		panic(http.ErrAbortHandler)
	}
}
func streamID(r *http.Request) (string, bool) {
	if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/stream") || !strings.HasPrefix(r.URL.Path, "/v1/bindings/") {
		return "", false
	}
	id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/v1/bindings/"), "/stream")
	return id, validID(id)
}

func streamStop(err error) tracesession.Stop {
	switch {
	case errors.Is(err, admission.ErrExpired):
		return tracesession.Stop(trace.Expired)
	case errors.Is(err, admission.ErrTargetChanged):
		return tracesession.Stop(trace.TargetChanged)
	case errors.Is(err, admission.ErrDenied):
		return tracesession.Stop(trace.AuthorisationLost)
	case errors.Is(err, context.Canceled):
		return tracesession.Stop(trace.Cancelled)
	default:
		return tracesession.Stop(trace.EngineFailed)
	}
}
