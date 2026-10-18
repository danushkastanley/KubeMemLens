package traceadmission

import (
	"context"
	"errors"
	"testing"

	"k8s.io/apiserver/pkg/authentication/user"
)

type previewPermissions struct{ denied string }

func (p *previewPermissions) Trace(_ context.Context, _ user.Info, op Operation, _, _ string) error {
	if string(op) == p.denied {
		return ErrDenied
	}
	return nil
}
func (p *previewPermissions) Pod(context.Context, user.Info, string, string) error {
	if p.denied == "pod" {
		return ErrDenied
	}
	return nil
}

func TestPreflightNeedsEveryPermissionBeforeAndAfterInspection(t *testing.T) {
	for _, permission := range []string{string(Inspect), string(Create), "pod"} {
		for _, stage := range []string{"before", "after"} {
			t.Run(permission+"/"+stage, func(t *testing.T) {
				h := newHarness(t, DefaultPolicy())
				permissions := &previewPermissions{}
				if stage == "before" {
					permissions.denied = permission
				}
				h.manager.deps.Authorizer = permissions
				b := &previewBinder{testBinder: h.binder, inspect: func(context.Context, Workload, Request) (NodePreflight, error) {
					permissions.denied = permission
					return previewResult(), nil
				}}
				h.manager.deps.Binder = b
				r, err := decodeSelected(t, selectedRequestBody())
				if err != nil {
					t.Fatal(err)
				}
				result, err := h.manager.Preflight(context.Background(), actor("operator"), r)
				if !errors.Is(err, ErrDenied) || result.EngineDigest != "" {
					t.Fatal("permission denial disclosed preflight", err)
				}
				if stage == "before" && b.calls != 0 {
					t.Fatal("inspection happened before authority was checked")
				}
				if stage == "after" && b.calls != 1 {
					t.Fatal("test did not exercise disclosure-time permission check")
				}
			})
		}
	}
}
