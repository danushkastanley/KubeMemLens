package client

import (
	"errors"
	"fmt"

	"github.com/danushkastanley/kube-memlens/internal/capability"
)

type ReadErrorKind string

const (
	ReadErrorForbidden   ReadErrorKind = "forbidden"
	ReadErrorNotFound    ReadErrorKind = "not_found"
	ReadErrorUnavailable ReadErrorKind = "unavailable"
	ReadErrorUnexpected  ReadErrorKind = "unexpected_response"
)

type ReadError struct {
	Kind       ReadErrorKind
	Operation  string
	StatusCode int
	// APINotInstalled marks a not-found response where API discovery also
	// reported that the KubeMemLens API group is absent from the cluster.
	APINotInstalled bool
	Cause           error

	// discoveryChecked records that a discovery request followed this 404,
	// and discoveryErr holds its result, so callers do not repeat it.
	discoveryChecked bool
	discoveryErr     error
}

// notFoundDiscovery returns the discovery result recorded with a 404, if any.
func notFoundDiscovery(err error) (discoveryErr error, checked bool) {
	var readErr *ReadError
	if !errors.As(err, &readErr) || readErr.Kind != ReadErrorNotFound {
		return nil, false
	}
	return readErr.discoveryErr, readErr.discoveryChecked
}

func (e *ReadError) Error() string {
	switch e.Kind {
	case ReadErrorForbidden:
		return "KubeMemLens access is forbidden for the requested scope"
	case ReadErrorNotFound:
		return "the requested KubeMemLens object was not found"
	case ReadErrorUnavailable:
		return "the KubeMemLens aggregated API is unavailable"
	default:
		return "the KubeMemLens aggregated API returned an unexpected response"
	}
}

func (e *ReadError) Unwrap() error {
	return e.Cause
}

func IsForbidden(err error) bool {
	var selection *capability.SelectionError
	if errors.As(err, &selection) {
		return selection.Reason == capability.AccessDenied || selection.Reason == capability.AuthenticationFailed
	}
	return hasReadErrorKind(err, ReadErrorForbidden)
}

func IsNotFound(err error) bool {
	return hasReadErrorKind(err, ReadErrorNotFound)
}

// IsAPINotInstalled reports whether the cluster does not serve the
// KubeMemLens API at all, which usually means the chart is not installed.
func IsAPINotInstalled(err error) bool {
	var readErr *ReadError
	return errors.As(err, &readErr) && readErr.Kind == ReadErrorNotFound && readErr.APINotInstalled
}

func IsUnavailable(err error) bool {
	return hasReadErrorKind(err, ReadErrorUnavailable)
}

func hasReadErrorKind(err error, kind ReadErrorKind) bool {
	var readErr *ReadError
	return errors.As(err, &readErr) && readErr.Kind == kind
}

func readTransportError(operation string, err error) error {
	return &ReadError{Kind: ReadErrorUnavailable, Operation: operation, Cause: err}
}

func readStatusError(operation string, status int) error {
	kind := ReadErrorUnexpected
	switch {
	case status == 401 || status == 403:
		kind = ReadErrorForbidden
	case status == 404:
		kind = ReadErrorNotFound
	case status == 408 || status == 429 || status >= 500:
		kind = ReadErrorUnavailable
	}
	return &ReadError{Kind: kind, Operation: operation, StatusCode: status}
}

func readDecodeError(operation string, err error) error {
	return &ReadError{
		Kind:      ReadErrorUnexpected,
		Operation: operation,
		Cause:     fmt.Errorf("decode response: %w", err),
	}
}

// readFailureDetail names what was wrong with a response. Status codes and
// decode failures come from this client and carry no cluster data; a transport
// cause is never printed.
func readFailureDetail(err error) string {
	var readErr *ReadError
	if !errors.As(err, &readErr) {
		return err.Error()
	}
	switch {
	case readErr.StatusCode != 0:
		return fmt.Sprintf("%s: HTTP %d", readErr.Operation, readErr.StatusCode)
	case readErr.Kind == ReadErrorUnexpected && readErr.Cause != nil:
		return readErr.Operation + ": " + readErr.Cause.Error()
	default:
		return readErr.Error()
	}
}
