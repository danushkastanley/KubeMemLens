//go:build !linux

package main

import (
	"context"
	"errors"

	"github.com/danushkastanley/kube-memlens/extensions/trace/admissionapi"
	admission "github.com/danushkastanley/kube-memlens/internal/traceadmission"
)

func configureWorker(context.Context, string, string, string, string) (installedRuntime, error) {
	return nil, errors.New("incident worker installation requires Linux")
}
func configureStream(_, _ string, base admission.Policy) (*admissionapi.StreamProxy, admission.Policy, error) {
	return nil, base, errors.New("incident worker installation requires Linux")
}
