package main

import (
	"context"
	"io"
	"strings"
	"testing"
)

func TestConfirmedPathsNeedAcceptedInstallation(t *testing.T) {
	err := run(context.Background(), []string{"admission-api", "--allow-confirmed-paths"}, io.Discard, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "confirmed paths require") {
		t.Fatal("raw paths enabled without accepted stream")
	}
}

func TestMissingAcceptanceCannotEnableNodeWorker(t *testing.T) {
	err := run(context.Background(), []string{"binding-node", "--node-uid", "fixture", "--node-name", "fixture", "--kubelet-cgroup-root", "/fixture", "--acceptance-policy", "/absent-policy"}, io.Discard, io.Discard)
	if err == nil {
		t.Fatal("missing installation enabled worker")
	}
}
