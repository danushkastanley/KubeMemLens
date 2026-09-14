package targetfs

import (
	"context"
	"github.com/danushkastanley/kube-memlens/internal/trace"
)

// Handle exposes immutable identity and owned lifecycle operations. The concrete
// descriptor and path storage cannot be copied or inspected through this API.
type Handle interface {
	Target() trace.TargetIdentity
	Check(context.Context) error
	Close() error
}
