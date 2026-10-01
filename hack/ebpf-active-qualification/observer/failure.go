package main

import (
	"errors"
	"io"
	"strconv"
)

// Never retain an underlying transport error: it can contain a URL or identity.
type observationFailure struct {
	stage  string
	cause  error
	status int
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

func (f observationFailure) Error() string {
	stage := safeStage(f.stage)
	if stage == "collector-status" && f.status >= 100 && f.status <= 599 {
		return stage + " http=" + strconv.Itoa(f.status)
	}
	return stage
}
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

func failureDetail(err error) string {
	var failure observationFailure
	if errors.As(err, &failure) {
		return failure.Error()
	}
	return "unclassified"
}
