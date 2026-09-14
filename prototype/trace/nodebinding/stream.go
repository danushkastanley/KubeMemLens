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
	"github.com/danushkastanley/kube-memlens/internal/tracepreflight"
	"github.com/danushkastanley/kube-memlens/internal/tracesession"
	"github.com/danushkastanley/kube-memlens/prototype/trace/streamhttp"
)

// Runtime is installation-owned programme selection. Prepare must verify its
// approved artifact and return a fresh adapter without attaching/loading work.
// The production command currently supplies nil: no incident programme approved.
type Runtime interface {
	Prepare(context.Context, trace.Specification) (*trace.Engine, string, error)
}
type streamRequest struct {
	Deadline time.Time `json:"deadline"`
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
	if r.Header.Get("Content-Type") != "application/json" || decode(http.MaxBytesReader(w, r.Body, maxBody), &request, "deadline") != nil {
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
	s.mu.Unlock()
	engine, digest, err := s.runtime.Prepare(initial, spec)
	if err != nil || engine == nil {
		cancel()
		writeError(w, admission.ErrUnavailable)
		return
	}
	// Validate before claiming the one-use execution or writing headers.
	now := time.Now().UTC()
	if _, err := traceframe.NewMetadata(traceframe.Metadata{SessionID: id, EngineDigest: tracepreflight.Baseline().EngineDigest, ProgrammeDigest: digest, Specification: spec, SessionStartedAt: now, Deadline: request.Deadline}); err != nil {
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
	session, err := tracesession.New(traceframe.Metadata{SessionID: id, EngineDigest: tracepreflight.Baseline().EngineDigest, ProgrammeDigest: digest, Specification: spec}, engine, sink, func(ctx context.Context) error {
		if err := execution.handle.Check(ctx); err != nil {
			return tracesession.Stop(trace.TargetChanged)
		}
		return nil
	})
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
	scope, stopDeadline := context.WithDeadlineCause(context.WithoutCancel(execution.ctx), request.Deadline, tracesession.Stop(trace.Expired))
	scope, stopScope := context.WithCancelCause(scope)
	stopExecution := context.AfterFunc(execution.ctx, func() { stopScope(streamStop(context.Cause(execution.ctx))) })
	defer func() { stopExecution(); stopScope(tracesession.Stop(trace.Cancelled)); stopDeadline() }()
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
