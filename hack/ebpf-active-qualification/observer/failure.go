package main

import (
	"errors"
	"io"
)

// Never retain an underlying transport error: it can contain a URL or identity.
type observationFailure struct {
	stage string
	cause error
}

func safeStage(stage string) string {
	switch stage {
	case "boot-binding", "agent-binding", "collector-binding", "collector-client",
		"agent-child", "agent-projection", "agent-transport", "agent-status", "agent-body", "agent-metrics",
		"collector-transport", "collector-status", "collector-body", "collector-envelope", "collector-metrics",
		"clock", "usage", "output-encoding", "output-budget", "output-write":
		return stage
	default:
		return "unclassified"
	}
}

func (f observationFailure) Error() string { return safeStage(f.stage) }
func (f observationFailure) Unwrap() error { return f.cause }

func atStage(stage string, err error) error {
	var failure observationFailure
	if errors.As(err, &failure) {
		return failure
	}
	failure = observationFailure{stage: stage}
	if errors.Is(err, io.ErrShortWrite) {
		failure.cause = io.ErrShortWrite
	}
	return failure
}

func failureStage(err error) string {
	var failure observationFailure
	if errors.As(err, &failure) {
		return safeStage(failure.stage)
	}
	return "unclassified"
}
