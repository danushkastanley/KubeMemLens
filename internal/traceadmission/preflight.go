package traceadmission

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	"github.com/danushkastanley/kube-memlens/internal/traceframe"
	"github.com/danushkastanley/kube-memlens/internal/tracepreflight"
	"k8s.io/apiserver/pkg/authentication/user"
)

// NodePreflight describes a checked, non-activated target and the node's observed
// startup baseline. It is not a reservation or permission to bypass admission.
type NodePreflight struct {
	Baseline        tracepreflight.Report `json:"baseline"`
	EngineDigest    string                `json:"engineDigest"`
	ProgrammeDigest string                `json:"programmeDigest"`
	StreamVersion   int                   `json:"streamVersion"`
}

func (NodePreflight) Format(w fmt.State, _ rune) { _, _ = io.WriteString(w, "[trace preflight]") }

func (p NodePreflight) Validate(kind trace.Kind) error {
	if _, err := tracepreflight.Encode(p.Baseline); err != nil || p.Baseline.State != tracepreflight.Supported || !traceframe.AllowsKind(p.StreamVersion, kind) {
		return ErrUnavailable
	}
	for _, digest := range []string{p.EngineDigest, p.ProgrammeDigest} {
		if len(digest) != 71 || !strings.HasPrefix(digest, "sha256:") || strings.Trim(digest[7:], "0123456789abcdef") != "" {
			return ErrUnavailable
		}
	}
	return nil
}

type Preflighter interface {
	// Preflight must release temporary handles before returning. It cannot load
	// or attach an incident programme, create a lease or reserve a replay nonce.
	Preflight(context.Context, Workload, Request) (NodePreflight, error)
}

// Preflight performs the same policy and current-identity checks as admission,
// without creating an entry or consuming trace quota. Admission repeats them.
func (m *Manager) Preflight(ctx context.Context, info user.Info, request Request) (result NodePreflight, err error) {
	if !m.beginOperation() {
		return result, ErrUnavailable
	}
	defer m.inflight.Done()
	principalClass := "unauthenticated"
	defer func() { m.record(Inspect, principalClass, request.kind, err) }()
	principal, _, err := snapshotPrincipal(info)
	if err != nil {
		return result, err
	}
	principalClass = principalCategory(principal)
	ctx, stop := m.operationContext(ctx)
	defer stop()
	if request.selection == nil {
		return result, ErrInvalidRequest
	}
	if err = m.policy.permits(request); err != nil {
		return result, err
	}
	if err = m.preflightAccess(ctx, principal, request); err != nil {
		return result, err
	}
	preflighter, ok := m.deps.Binder.(Preflighter)
	if !ok {
		return result, ErrUnavailable
	}
	workload, err := m.deps.Resolver.Resolve(ctx, request)
	if err != nil {
		return result, safeDependencyError(err)
	}
	if err = validateWorkload(request, workload); err != nil {
		return result, err
	}
	result, err = preflighter.Preflight(ctx, workload, request)
	if err != nil {
		return NodePreflight{}, safeDependencyError(err)
	}
	if result.Validate(request.Kind()) != nil || result.EngineDigest != m.policy.EngineDigest {
		return NodePreflight{}, ErrUnavailable
	}
	if err = m.deps.Resolver.Revalidate(ctx, workload); err != nil {
		return NodePreflight{}, safeDependencyError(err)
	}
	if err = m.preflightAccess(ctx, principal, request); err != nil {
		return NodePreflight{}, err
	}
	if ctx.Err() != nil {
		return NodePreflight{}, ErrUnavailable
	}
	return result, nil
}

func (m *Manager) preflightAccess(ctx context.Context, principal user.Info, request Request) error {
	if err := m.deps.Authorizer.Trace(ctx, principal, Inspect, request.Namespace(), ""); err != nil {
		return safeDependencyError(err)
	}
	return m.access(ctx, principal, Create, request, "")
}
