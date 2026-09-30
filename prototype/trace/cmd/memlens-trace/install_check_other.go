//go:build !linux && !darwin

package main

import (
	"context"
	"io"

	"github.com/danushkastanley/kube-memlens/prototype/trace/installcheck"
)

func runInstallCheck(context.Context, []string, io.Writer, io.Writer) error {
	return installcheck.ErrUnavailable
}
