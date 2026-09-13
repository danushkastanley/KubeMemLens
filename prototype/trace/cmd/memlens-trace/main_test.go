package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	p "github.com/danushkastanley/kube-memlens/internal/tracepreflight"
)

func TestInvalidCommandNeverStartsWorker(t *testing.T) {
	for _, args := range [][]string{nil, {"run", "trace_exec"}, {"doctor", "--timeout", "20s"}, {"doctor", "--timeout", "0s"}, {"doctor", "node/other"}, {"doctor", "--node", "other"}} {
		var output bytes.Buffer
		if err := run(context.Background(), args, &output, io.Discard); err == nil {
			t.Fatal("unreviewed command accepted")
		}
		if output.Len() != 0 {
			t.Fatal("invalid command emitted a report")
		}
	}
}

func TestWorkerOutputCannotExceedTheReportBudget(t *testing.T) {
	var buffer boundedBuffer
	if _, err := buffer.Write([]byte("prefix")); err != nil {
		t.Fatal(err)
	}
	if _, err := buffer.Write(make([]byte, p.MaxReportBytes+1)); err == nil {
		t.Fatal("oversized worker output accepted")
	}
	if buffer.String() != "prefix" {
		t.Fatal("oversized output was retained")
	}
}

type shortWriter struct{}

func (shortWriter) Write([]byte) (int, error) { return 0, nil }

func TestOutputFailuresAndInvalidReportsAreNotSuccess(t *testing.T) {
	r := p.Failed(p.ProbeTimeout)
	if err := writeJSON(shortWriter{}, r); !errors.Is(err, io.ErrShortWrite) {
		t.Fatal("short write became success")
	}
	r.Checks[0].Value = "private\x1b[2J"
	var output bytes.Buffer
	if err := writeText(&output, r); err == nil || output.Len() != 0 {
		t.Fatal("invalid data reached terminal output")
	}
}

func TestBaselineOutputDoesNotImplyIncidentTracingApproval(t *testing.T) {
	var output bytes.Buffer
	if err := writeText(&output, p.Failed(p.ProbeTimeout)); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "Incident tracing remains unavailable") || !strings.Contains(output.String(), "probe_timeout") {
		t.Fatal("baseline limitation or timeout is hidden")
	}
}
