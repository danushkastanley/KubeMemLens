package main

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
)

func TestFailureStagesNeverExposeUnderlyingErrors(t *testing.T) {
	private := errors.New("Bearer secret-token https://private.example/tenant/path")
	for _, stage := range []string{"collector-transport", "agent-child", private.Error()} {
		err := atStage(stage, private)
		for _, value := range []string{err.Error(), fmt.Sprintf("%+v", err), failureStage(err), failureDetail(err)} {
			if strings.Contains(value, "secret-token") || strings.Contains(value, "private.example") || strings.Contains(value, "tenant/path") {
				t.Fatal("private error escaped")
			}
		}
		if errors.Unwrap(err) != nil {
			t.Fatal("private cause retained")
		}
	}
	if failureStage(private) != "unclassified" || failureDetail(private) != "unclassified" {
		t.Fatal("unclassified errors must remain opaque")
	}
}

func TestFailureDetailOnlyIncludesBoundedCollectorStatus(t *testing.T) {
	for _, failure := range []observationFailure{
		{stage: "collector-status", status: -1},
		{stage: "collector-status", status: 600},
		{stage: "collector-transport", status: 503},
		{stage: "https://private.example", status: 401},
	} {
		if failureDetail(failure) != safeStage(failure.stage) {
			t.Fatal("unexpected failure detail")
		}
	}
	err := atStage("collector-transport", observationFailure{stage: "collector-status", status: 503})
	if failureStage(err) != "collector-status" || failureDetail(err) != "collector-status http=503" {
		t.Fatal("numeric HTTP status lost")
	}
}

func TestFailureStagePreservesSpecificCategoryAndShortWrites(t *testing.T) {
	err := atStage("agent-child", atStage("agent-status", errObservation))
	if failureStage(err) != "agent-status" {
		t.Fatal("child failure category lost")
	}
	err = atStage("output-write", io.ErrShortWrite)
	if !errors.Is(err, io.ErrShortWrite) || failureStage(err) != "output-write" {
		t.Fatal("short write semantics lost")
	}
}
