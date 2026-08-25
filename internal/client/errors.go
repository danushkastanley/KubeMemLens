package client

import (
	"errors"
	"fmt"
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
	Cause      error
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
	return hasReadErrorKind(err, ReadErrorForbidden)
}

func IsNotFound(err error) bool {
	return hasReadErrorKind(err, ReadErrorNotFound)
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
