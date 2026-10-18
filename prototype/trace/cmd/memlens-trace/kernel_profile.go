package main

import (
	"errors"

	p "github.com/danushkastanley/kube-memlens/internal/tracepreflight"
	"github.com/danushkastanley/kube-memlens/prototype/trace/nodeprofile"
)

func checkRuntimeKernel(report p.Report, expected string) error {
	if report.State != p.Supported {
		return errors.New("binding node preflight is incomplete or unsupported")
	}
	if expected == "" {
		return nil // Historical prototype installations used the baseline only.
	}
	mismatch := errors.New("binding node does not match the pinned kernel release")
	if !nodeprofile.ValidKernel(expected) {
		return mismatch
	}
	count := 0
	for _, check := range report.Checks {
		if check.ID != p.Kernel {
			continue
		}
		count++
		if check.State != p.Supported || check.Reason != p.Available || check.Value != expected {
			return mismatch
		}
	}
	if count != 1 {
		return mismatch
	}
	return nil
}
