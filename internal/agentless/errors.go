package agentless

import (
	"context"
	"errors"

	"github.com/danushkastanley/kube-memlens/internal/capability"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

const limitReached capability.Reason = "limit-reached"

func queryFailure(reason capability.Reason, cause error) error {
	return &capability.SelectionError{Mode: capability.Restricted, Reason: reason, Cause: cause}
}

func readFailure(err error) error {
	reason := capability.RequestFailed
	switch {
	case apierrors.IsForbidden(err):
		reason = capability.AccessDenied
	case apierrors.IsUnauthorized(err):
		reason = capability.AuthenticationFailed
	case apierrors.IsNotFound(err):
		reason = capability.SourceAbsent
	case errors.Is(err, context.Canceled):
		reason = "request-cancelled"
	case errors.Is(err, context.DeadlineExceeded):
		reason = "request-timed-out"
	case errors.Is(err, errResponseLimit), errors.Is(err, errTotalLimit), errors.Is(err, errRequestLimit):
		reason = limitReached
	}
	return queryFailure(reason, err)
}
