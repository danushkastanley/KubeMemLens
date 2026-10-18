package traceadmission

import (
	"context"
	"errors"
	"sync"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	"github.com/danushkastanley/kube-memlens/internal/traceaudit"
)

// event is frozen before admission leaves initialisation. terminal joins any
// concurrent stream/cleanup reporting and prevents duplicate terminal records.
type auditLifecycle struct {
	event       traceaudit.Event
	terminal    sync.Once
	terminalErr error
}

func (m *Manager) recordTerminal(lifecycle *auditLifecycle, reason trace.Termination, source traceaudit.OutcomeSource) error {
	lifecycle.terminal.Do(func() {
		event := lifecycle.event
		event.Operation, event.Decision, event.OutcomeSource = traceaudit.Terminal, traceaudit.Observed, source
		event.Reason = terminalAuditReason(reason)
		lifecycle.terminalErr = m.emitAudit(event)
	})
	return lifecycle.terminalErr
}

// RecordTerminal accepts only a validated upstream summary or the controller's
// fixed terminal decision. Neither includes raw events or claims kernel cleanup.
func (l *Lease) RecordTerminal(reason trace.Termination, source traceaudit.OutcomeSource) error {
	return l.manager.recordTerminal(l.audit, reason, source)
}
func terminalAuditReason(reason trace.Termination) traceaudit.Reason {
	switch reason {
	case trace.Expired:
		return traceaudit.Expired
	case trace.Cancelled:
		return traceaudit.Cancelled
	case trace.EventLimit:
		return traceaudit.EventLimit
	case trace.OutputLimit:
		return traceaudit.OutputLimit
	case trace.TargetChanged:
		return traceaudit.TargetChanged
	case trace.AuthorisationLost:
		return traceaudit.Denied
	default:
		return traceaudit.EngineFailed
	}
}
func terminationFromCause(cause error) trace.Termination {
	switch {
	case errors.Is(cause, ErrExpired):
		return trace.Expired
	case errors.Is(cause, ErrDenied):
		return trace.AuthorisationLost
	case errors.Is(cause, ErrTargetChanged):
		return trace.TargetChanged
	case errors.Is(cause, context.Canceled):
		return trace.Cancelled
	default:
		return trace.EngineFailed
	}
}
