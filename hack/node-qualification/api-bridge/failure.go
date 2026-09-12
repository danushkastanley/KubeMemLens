package main

import (
	"context"
	"errors"

	"github.com/danushkastanley/kube-memlens/internal/client"
)

// Exit codes carry only a fixed category across the private process boundary.
// Response bodies, target addresses and credential-plugin errors stay private.
func failureExitCode(err error) int {
	if errors.Is(err, context.DeadlineExceeded) {
		return 15
	}
	if errors.Is(err, context.Canceled) {
		return 16
	}
	var readErr *client.ReadError
	if !errors.As(err, &readErr) {
		return 1
	}
	switch readErr.StatusCode {
	case 401:
		return 10
	case 403:
		return 11
	case 404:
		return 12
	case 429:
		return 13
	case 408:
		return 15
	}
	if readErr.StatusCode >= 500 {
		return 14
	}
	if readErr.Kind == client.ReadErrorUnavailable {
		return 17
	}
	return 18
}
