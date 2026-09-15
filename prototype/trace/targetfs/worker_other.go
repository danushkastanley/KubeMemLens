//go:build !linux

package targetfs

import (
	"context"
	"os"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	admission "github.com/danushkastanley/kube-memlens/internal/traceadmission"
)

func ExportForWorker(context.Context, Handle) (*os.File, error) {
	return nil, admission.ErrUnavailable
}
func VerifyWorkerDescriptor(context.Context, *os.File, trace.TargetIdentity) error {
	return admission.ErrUnavailable
}
func ReadWorkerMemoryStat(context.Context, *os.File, trace.TargetIdentity) ([]byte, error) {
	return nil, admission.ErrUnavailable
}
