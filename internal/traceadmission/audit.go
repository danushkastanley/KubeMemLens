package traceadmission

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	"github.com/danushkastanley/kube-memlens/internal/traceaudit"
	"k8s.io/apiserver/pkg/authentication/user"
)

func policyFingerprint(policy Policy) (string, error) {
	data, err := json.Marshal(struct {
		Version int
		Policy  Policy
	}{1, policy})
	if err != nil {
		return "", ErrInvalidRequest
	}
	digest := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}
func (m *Manager) auditLimits(bounds trace.Bounds) *traceaudit.Limits {
	p := m.policy
	return &traceaudit.Limits{DurationNanos: int64(bounds.Duration), Events: bounds.Events, OutputBytes: bounds.OutputBytes, MapBytes: bounds.MapBytes, PathBytes: bounds.PathBytes, PerActor: p.PerPrincipal, PerTenant: p.PerNamespace, PerNode: p.PerNode, Cluster: p.Global, PendingTTLNanos: int64(p.PendingTTL), ResultRetentionNanos: 0}
}
func (m *Manager) auditEvent(operation Operation, request Request) traceaudit.Event {
	event := traceaudit.Event{KeyID: m.deps.AuditReferences.KeyID(), PolicyRef: m.policyRef, ActorClass: "unauthenticated", Operation: traceaudit.Operation(operation)}
	if trace.ValidateIntent(request.kind, request.paths, request.bounds) == nil {
		event.Kind = request.kind
		event.Limits = m.auditLimits(request.bounds)
	}
	return event
}
func (m *Manager) identifyAudit(event *traceaudit.Event, principal user.Info, namespace, id string) error {
	actor, err := m.deps.AuditReferences.Actor(principal.GetName(), principal.GetUID())
	if err != nil {
		return ErrUnavailable
	}
	event.ActorClass, event.ActorRef = principalCategory(principal), actor
	if validNamespace(namespace) {
		event.TenantRef, _ = m.deps.AuditReferences.Tenant(namespace)
	}
	if id != "" {
		event.SessionRef, _ = m.deps.AuditReferences.Session(id)
	}
	return nil
}
func auditEntry(event *traceaudit.Event, e *entry) {
	event.TargetRef, event.SessionRef, event.Kind, event.Limits = e.audit.event.TargetRef, e.audit.event.SessionRef, e.audit.event.Kind, e.audit.event.Limits
}
func (m *Manager) AuditHealthy() error {
	if m.auditFailed.Load() {
		return ErrUnavailable
	}
	return nil
}
func (m *Manager) emitAudit(event traceaudit.Event) error {
	if m.AuditHealthy() != nil {
		return ErrUnavailable
	}
	event.Time = time.Now().UTC()
	record, err := traceaudit.NewRecord(event)
	if err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		err = m.deps.Audit(ctx, record)
	}
	if err != nil {
		m.auditFailed.Store(true)
		return ErrUnavailable
	}
	return nil
}
func (m *Manager) record(event traceaudit.Event, err error) error {
	event.Decision = traceaudit.Accepted
	switch Operation(event.Operation) {
	case Create:
		event.Reason = traceaudit.Admitted
	case Read:
		event.Reason = traceaudit.Revalidated
	case Attach:
		event.Reason = traceaudit.Attached
	case Cancel:
		event.Reason = traceaudit.Cancelled
	case Inspect:
		event.Reason = traceaudit.Checked
	}
	if err != nil {
		event.Decision = traceaudit.Rejected
		event.Reason = auditReason(err)
	}
	return m.emitAudit(event)
}
func auditReason(err error) traceaudit.Reason {
	switch {
	case errors.Is(err, ErrUnauthenticated):
		return traceaudit.Unauthenticated
	case errors.Is(err, ErrDenied):
		return traceaudit.Denied
	case errors.Is(err, ErrCapacity):
		return traceaudit.Capacity
	case errors.Is(err, ErrExpired):
		return traceaudit.Expired
	case errors.Is(err, ErrTargetChanged):
		return traceaudit.TargetChanged
	case errors.Is(err, ErrNotFound):
		return traceaudit.NotFound
	case errors.Is(err, ErrInvalidRequest):
		return traceaudit.InvalidRequest
	default:
		return traceaudit.Unavailable
	}
}
