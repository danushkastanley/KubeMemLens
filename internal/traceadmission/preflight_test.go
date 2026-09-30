package traceadmission

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/tracepreflight"
	"k8s.io/apiserver/pkg/authentication/user"
)

type previewBinder struct {
	*testBinder
	inspect func(context.Context, Workload, Request) (NodePreflight, error)
	calls   int
}

type inspectionAuthorizer struct {
	*testAuthorizer
	revoked bool
}

func (a *inspectionAuthorizer) Trace(ctx context.Context, p user.Info, op Operation, namespace, name string) error {
	if op == Inspect && a.revoked {
		return ErrDenied
	}
	return a.testAuthorizer.Trace(ctx, p, op, namespace, name)
}

func TestPreflightRechecksItsOwnPermissionAfterNodeInspection(t *testing.T) {
	h := newHarness(t, DefaultPolicy())
	a := &inspectionAuthorizer{testAuthorizer: h.auth}
	h.manager.deps.Authorizer = a
	h.manager.deps.Binder = &previewBinder{testBinder: h.binder, inspect: func(context.Context, Workload, Request) (NodePreflight, error) {
		a.revoked = true
		return previewResult(), nil
	}}
	r, err := decodeSelected(t, selectedRequestBody())
	if err != nil {
		t.Fatal(err)
	}
	result, err := h.manager.Preflight(context.Background(), actor("operator"), r)
	if !errors.Is(err, ErrDenied) || result.EngineDigest != "" {
		t.Fatal("revoked preflight permission disclosed report", err)
	}
}

func (b *previewBinder) Preflight(ctx context.Context, w Workload, r Request) (NodePreflight, error) {
	b.calls++
	return b.inspect(ctx, w, r)
}

func previewResult() NodePreflight {
	profile := tracepreflight.Baseline()
	report := tracepreflight.Report{SchemaVersion: 1, Scope: profile.Scope, ProfileDigest: profile.Digest(), TraceApproval: "pending-custom-programme-freeze", CapturedAt: time.Now().UTC(), State: tracepreflight.Supported}
	for _, id := range profile.Checks {
		report.Checks = append(report.Checks, tracepreflight.Check{ID: id, State: tracepreflight.Supported, Reason: tracepreflight.Available})
	}
	return NodePreflight{Baseline: report, EngineDigest: profile.EngineDigest, ProgrammeDigest: "sha256:" + strings.Repeat("a", 64), StreamVersion: 2}
}

func TestPreflightDoesNotCreateAdmissionOrBindTarget(t *testing.T) {
	h := newHarness(t, DefaultPolicy())
	b := &previewBinder{testBinder: h.binder, inspect: func(context.Context, Workload, Request) (NodePreflight, error) { return previewResult(), nil }}
	h.manager.deps.Binder = b
	r, err := decodeSelected(t, selectedRequestBody())
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		result, err := h.manager.Preflight(context.Background(), actor("operator"), r)
		if err != nil || result.Validate(r.Kind()) != nil {
			t.Fatal("preflight failed", err)
		}
	}
	h.manager.mu.Lock()
	count := len(h.manager.entries)
	h.manager.mu.Unlock()
	if count != 0 || h.binder.calls.Load() != 0 || b.calls != 3 {
		t.Fatal("preflight created trace state")
	}
	if _, err := h.manager.Admit(context.Background(), actor("operator"), r); err != nil {
		t.Fatal("preflight consumed trace quota", err)
	}
}

func TestPreflightRechecksAuthorityAndTargetBeforeDisclosure(t *testing.T) {
	for _, scenario := range []string{"denied", "changed-before", "revoked-after", "changed-after", "wrong-engine", "unsupported", "invalid-version", "cancelled"} {
		t.Run(scenario, func(t *testing.T) {
			h := newHarness(t, DefaultPolicy())
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			b := &previewBinder{testBinder: h.binder}
			b.inspect = func(context.Context, Workload, Request) (NodePreflight, error) {
				result := previewResult()
				switch scenario {
				case "revoked-after":
					h.auth.denied.Store(true)
				case "changed-after":
					h.resolver.changed.Store(true)
				case "wrong-engine":
					result.EngineDigest = "sha256:" + strings.Repeat("b", 64)
				case "unsupported":
					result.Baseline = tracepreflight.Failed(tracepreflight.ProbeFailed)
				case "invalid-version":
					result.StreamVersion = 99
				case "cancelled":
					cancel()
				}
				return result, nil
			}
			h.manager.deps.Binder = b
			body := selectedRequestBody()
			if scenario == "denied" {
				h.auth.denied.Store(true)
			}
			if scenario == "changed-before" {
				body["expectedPodUID"] = "replaced"
			}
			r, err := decodeSelected(t, body)
			if err != nil {
				t.Fatal(err)
			}
			result, err := h.manager.Preflight(ctx, actor("operator"), r)
			if err == nil || result.EngineDigest != "" || h.binder.calls.Load() != 0 {
				t.Fatal("failed preflight disclosed readiness or bound a target")
			}
			if (scenario == "denied" || scenario == "changed-before") && b.calls != 0 {
				t.Fatal("failed early check reached node")
			}
		})
	}
}

func TestPreflightRequiresSelectionAndImplementation(t *testing.T) {
	h := newHarness(t, DefaultPolicy())
	if _, err := h.manager.Preflight(context.Background(), actor("operator"), requestFor(t, "tenant-a")); !errors.Is(err, ErrInvalidRequest) {
		t.Fatal(err)
	}
	r, err := decodeSelected(t, selectedRequestBody())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.manager.Preflight(context.Background(), actor("operator"), r); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
}
