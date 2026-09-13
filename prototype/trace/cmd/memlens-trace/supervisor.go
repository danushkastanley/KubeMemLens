package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"time"

	p "github.com/danushkastanley/kube-memlens/internal/tracepreflight"
)

type boundedBuffer struct{ bytes.Buffer }

func (b *boundedBuffer) Write(data []byte) (int, error) {
	if len(data) > p.MaxReportBytes+1-b.Len() {
		return 0, errors.New("preflight worker output exceeded bound")
	}
	return b.Buffer.Write(data)
}

// One fresh process per preflight keeps feature caches tied to the current
// credentials and bounds kernel calls that cannot observe a Go context.
func supervise(parent context.Context, bundle string, timeout time.Duration) p.Report {
	executable, err := os.Executable()
	if err != nil {
		return p.Failed(p.ProbeFailed)
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, "_probe", "--bundle", bundle, "--timeout", timeout.String())
	return executeWorker(ctx, cmd)
}

func executeWorker(ctx context.Context, cmd *exec.Cmd) p.Report {
	configureChild(cmd)
	cmd.WaitDelay = time.Second
	output := &boundedBuffer{}
	cmd.Stdout = output
	// Worker errors may contain kernel diagnostics. Return a stable failure code
	// instead of retaining or forwarding verifier text or crash output.
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return p.Failed(p.ProbeTimeout)
		}
		return p.Failed(p.ProbeFailed)
	}
	report, err := p.Decode(bytes.TrimSpace(output.Bytes()))
	if err != nil {
		return p.Failed(p.ProbeFailed)
	}
	return report
}
