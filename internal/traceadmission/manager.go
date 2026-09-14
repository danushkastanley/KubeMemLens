package traceadmission

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	"github.com/danushkastanley/kube-memlens/internal/tracepreflight"
	"k8s.io/apiserver/pkg/authentication/user"
)

type Dependencies struct {
	Authorizer Authorizer
	Resolver   Resolver
	Binder     Binder
	Audit      Audit
}
type stage uint8

const (
	pending stage = iota
	admitted
)

type entry struct {
	id       string
	owner    [32]byte
	request  Request
	nodeUID  string
	expires  time.Time
	stage    stage
	workload Workload
	binding  Binding
	result   Admission
}

type Manager struct {
	inflight    sync.WaitGroup
	shutdownErr error
	mu          sync.Mutex
	entries     map[string]*entry
	deps        Dependencies
	policy      Policy
	ctx         context.Context
	cancel      context.CancelFunc
	done        chan struct{}
	closed      bool
}

// NewManager starts one bounded expiry worker. Close terminates it and releases
// reservations. The controller owns this manager for its entire process lifetime.
func NewManager(ctx context.Context, deps Dependencies, policy Policy) (*Manager, error) {
	if ctx == nil || ctx.Err() != nil || deps.Authorizer == nil || deps.Resolver == nil || deps.Binder == nil || deps.Audit == nil {
		return nil, ErrUnavailable
	}
	if err := policy.validate(); err != nil {
		return nil, err
	}
	lifetime, cancel := context.WithCancel(ctx)
	m := &Manager{entries: map[string]*entry{}, deps: deps, policy: policy, ctx: lifetime, cancel: cancel, done: make(chan struct{})}
	go m.expiryLoop()
	return m, nil
}

func (m *Manager) Admit(ctx context.Context, info user.Info, request Request) (result Admission, err error) {
	if !m.beginOperation() {
		return result, ErrUnavailable
	}
	defer m.inflight.Done()
	principalClass := "unauthenticated"
	defer func() { m.record(Create, principalClass, request.kind, err) }()
	principal, owner, err := snapshotPrincipal(info)
	if err != nil {
		return result, err
	}
	principalClass = principalCategory(principal)
	ctx, stop := m.operationContext(ctx)
	defer stop()
	if err = m.policy.permits(request); err != nil {
		return result, err
	}
	if err = m.access(ctx, principal, Create, request, ""); err != nil {
		return result, err
	}
	reserved, err := m.reserve(owner, request)
	if err != nil {
		return result, err
	}
	defer func() {
		if err != nil {
			m.discard(reserved.id)
		}
	}()
	workload, err := m.deps.Resolver.Resolve(ctx, request)
	if err != nil {
		return result, safeDependencyError(err)
	}
	if err = validateWorkload(request, workload); err != nil {
		return result, err
	}
	if err = m.assignNode(reserved, workload.Target.NodeUID); err != nil {
		return result, err
	}
	binding, err := m.deps.Binder.Bind(ctx, reserved.id, workload, reserved.expires)
	if err != nil {
		return result, safeDependencyError(err)
	}
	if binding == nil {
		return result, ErrUnavailable
	}
	published := false
	defer func() {
		if !published {
			m.closeBindings([]Binding{binding})
		}
	}()
	target := binding.Target()
	if !sameLifetime(target, workload.Target) || binding.ProfileDigest() != tracepreflight.Baseline().Digest() {
		return result, ErrTargetChanged
	}
	specification, err := trace.NewSpecification(request.kind, target, request.paths, request.bounds)
	if err != nil {
		return result, ErrTargetChanged
	}
	if err = m.deps.Resolver.Revalidate(ctx, workload); err != nil {
		return result, safeDependencyError(err)
	}
	if err = binding.Revalidate(ctx); err != nil {
		return result, safeDependencyError(err)
	}
	if err = m.access(ctx, principal, Create, request, ""); err != nil {
		return result, err
	}
	if ctx.Err() != nil {
		return result, ErrUnavailable
	}
	result = Admission{id: reserved.id, specification: specification, engineDigest: tracepreflight.Baseline().EngineDigest, expiresAt: reserved.expires}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || m.entries[reserved.id] != reserved || !time.Now().Before(reserved.expires) {
		return Admission{}, ErrExpired
	}
	reserved.workload, reserved.binding, reserved.result, reserved.stage = workload, binding, result, admitted
	published = true
	return result, nil
}

func (m *Manager) access(ctx context.Context, p user.Info, operation Operation, r Request, resourceName string) error {
	if ctx.Err() != nil {
		return ErrUnavailable
	}
	if err := m.deps.Authorizer.Trace(ctx, p, operation, r.namespace, resourceName); err != nil {
		return safeDependencyError(err)
	}
	if err := m.deps.Authorizer.Pod(ctx, p, r.namespace, r.pod); err != nil {
		return safeDependencyError(err)
	}
	return nil
}

func (m *Manager) operationContext(parent context.Context) (context.Context, func()) {
	ctx, cancel := context.WithTimeout(parent, min(10*time.Second, m.policy.PendingTTL))
	stop := context.AfterFunc(m.ctx, cancel)
	return ctx, func() { stop(); cancel() }
}

func validateWorkload(r Request, w Workload) error {
	t := w.Target
	if t.ValidateLifetime() != nil || t.CgroupID != 0 || t.Namespace != r.namespace || t.PodName != r.pod || t.ContainerName != r.container || w.NodeName == "" {
		return ErrTargetChanged
	}
	switch w.QoS {
	case "Guaranteed", "Burstable", "BestEffort":
	default:
		return ErrUnavailable
	}
	return nil
}

func sameLifetime(a, b trace.TargetIdentity) bool {
	return a.Namespace == b.Namespace && a.PodName == b.PodName && a.PodUID == b.PodUID && a.ContainerName == b.ContainerName && a.ContainerID == b.ContainerID && a.ContainerStartedAt.Equal(b.ContainerStartedAt) && a.NodeUID == b.NodeUID
}

func safeDependencyError(err error) error {
	for _, allowed := range []error{ErrDenied, ErrCapacity, ErrExpired, ErrTargetChanged, ErrNotFound} {
		if errors.Is(err, allowed) {
			return allowed
		}
	}
	return ErrUnavailable
}

func newID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", ErrUnavailable
	}
	return hex.EncodeToString(value[:]), nil
}

func (m *Manager) record(operation Operation, principalClass string, kind trace.Kind, err error) {
	decision, reason := "accepted", "admitted"
	if operation == Read {
		reason = "revalidated"
	}
	if operation == Cancel {
		reason = "cancelled"
	}
	if err != nil {
		decision = "rejected"
		switch {
		case errors.Is(err, ErrUnauthenticated):
			reason = "unauthenticated"
		case errors.Is(err, ErrDenied):
			reason = "denied"
		case errors.Is(err, ErrCapacity):
			reason = "capacity"
		case errors.Is(err, ErrExpired):
			reason = "expired"
		case errors.Is(err, ErrTargetChanged):
			reason = "target_changed"
		case errors.Is(err, ErrNotFound):
			reason = "not_found"
		case errors.Is(err, ErrInvalidRequest):
			reason = "invalid_request"
		default:
			reason = "unavailable"
		}
	}
	m.deps.Audit(AuditEvent{Operation: operation, Decision: decision, Reason: reason, Principal: principalClass, Kind: kind})
}

func (m *Manager) beginOperation() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || m.ctx.Err() != nil {
		return false
	}
	m.inflight.Add(1)
	return true
}
