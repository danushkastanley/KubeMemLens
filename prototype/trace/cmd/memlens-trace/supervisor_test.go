package main

import (
	"context"
	p "github.com/danushkastanley/kube-memlens/internal/tracepreflight"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestWorkerDeadlineTerminatesAndReapsProcess(t *testing.T) {
	if os.Getenv("KML_TEST_HUNG_WORKER") == "1" {
		time.Sleep(time.Hour)
		os.Exit(99)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestWorkerDeadlineTerminatesAndReapsProcess$")
	cmd.Env = append(os.Environ(), "KML_TEST_HUNG_WORKER=1")
	started := time.Now()
	report := executeWorker(ctx, cmd)
	if report.Checks[0].Reason != p.ProbeTimeout {
		t.Fatalf("timeout report: %+v", report)
	}
	if time.Since(started) > 3*time.Second {
		t.Fatal("deadline did not bound worker lifetime")
	}
	if cmd.ProcessState == nil || cmd.ProcessState.Success() {
		t.Fatal("worker not reaped after termination")
	}
}
