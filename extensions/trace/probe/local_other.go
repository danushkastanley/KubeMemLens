//go:build !linux

package probe

import (
	"context"
	p "github.com/danushkastanley/kube-memlens/internal/tracepreflight"
)

type Local struct{}

func New(string) *Local { return &Local{} }
func (*Local) Probe(_ context.Context, id p.ID) p.Check {
	return p.Check{ID: id, State: p.Unsupported, Reason: p.PlatformUnsupported}
}
