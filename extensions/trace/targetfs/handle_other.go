//go:build !linux

package targetfs

import (
	"context"
	admission "github.com/danushkastanley/kube-memlens/internal/traceadmission"
)

func Resolve(context.Context, Config, admission.Workload) (Handle, error) {
	return nil, admission.ErrUnavailable
}
