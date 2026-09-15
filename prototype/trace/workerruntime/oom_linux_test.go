package workerruntime

import (
	"context"
	"errors"
	"os"
	"runtime"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	"github.com/danushkastanley/kube-memlens/internal/traceframe"
	"github.com/danushkastanley/kube-memlens/prototype/trace/filecache"
)

type fixtureOOMOutput struct{ fixtureOutput }

func (o *fixtureOOMOutput) OOMDecision(event trace.OOMDecision) error {
	if event.Scope != trace.OOMScopeCgroup {
		return ErrRuntime
	}
	o.events.Add(1)
	return nil
}

func TestOOMLauncherUsesAcceptedProgrammeAndVersion(t *testing.T) {
	r := fixtureRuntime(t)
	if _, err := r.configuration.ManifestSHA256(filecache.ArtifactID{Kind: trace.OOM, Architecture: runtime.GOARCH}); err != nil {
		t.Skip("requires a six-object review bundle")
	}
	exported := make(chan *os.File, 1)
	r.exportTarget = fixtureExporter(exported)
	base, handle := fixtureSpec(t, "oom-target")
	spec, err := trace.NewSpecification(trace.OOM, base.Target(), trace.OmitPaths, base.Bounds())
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := r.Prepare(context.Background(), spec, handle)
	if err != nil || prepared.StreamVersion != traceframe.OOMVersion {
		t.Fatal("OOM runtime selected wrong stream contract")
	}
	output := &fixtureOOMOutput{}
	result, err := prepared.Engine.Run(context.Background(), spec, output)
	if err != nil || output.events.Load() != 1 || result.Counts.Produced == nil || *result.Counts.Produced != 1 {
		t.Fatal("OOM launcher fixture failed")
	}
	if _, err := (<-exported).Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatal("OOM worker retained its target duplicate")
	}
}
