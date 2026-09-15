//go:build !linux

package main

import (
	"context"
	"errors"

	admission "github.com/danushkastanley/kube-memlens/internal/traceadmission"
	"github.com/danushkastanley/kube-memlens/prototype/trace/admissionapi"
)

func configureWorker(context.Context, string, string, string) (installedRuntime, error) {
	return nil, errors.New("incident worker installation requires Linux")
}
func configureStream(_ string, base admission.Policy) (*admissionapi.StreamProxy, admission.Policy, error) {
	return nil, base, errors.New("incident worker installation requires Linux")
}
