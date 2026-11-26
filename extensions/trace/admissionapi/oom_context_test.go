package admissionapi

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	admission "github.com/danushkastanley/kube-memlens/internal/traceadmission"
	"github.com/danushkastanley/kube-memlens/internal/traceframe"
)

type oomContextFunc func(context.Context, trace.TargetIdentity) (trace.OOMKubernetesSample, error)

func (f oomContextFunc) SampleOOMContext(ctx context.Context, target trace.TargetIdentity) (trace.OOMKubernetesSample, error) {
	return f(ctx, target)
}

func oomContextSpec(t *testing.T) trace.Specification {
	t.Helper()
	target := trace.TargetIdentity{Namespace: "tenant", PodName: "pod", PodUID: "uid", ContainerName: "worker", ContainerID: strings.Repeat("a", 64), ContainerStartedAt: time.Unix(10, 0).UTC(), NodeUID: "node", CgroupID: 42}
	spec, err := trace.NewSpecification(trace.OOM, target, trace.OmitPaths, trace.DefaultBounds())
	if err != nil {
		t.Fatal(err)
	}
	return spec
}

func TestOOMControlSamplesBracketTheNodeStream(t *testing.T) {
	spec := oomContextSpec(t)
	calls := 0
	source := oomContextFunc(func(_ context.Context, target trace.TargetIdentity) (trace.OOMKubernetesSample, error) {
		calls++
		at, zero := time.Now().UTC(), uint64(0)
		return trace.OOMKubernetesSample{Target: target, Start: at, End: at, Restarts: &zero, NodePressure: "false"}, nil
	})
	state, err := beginOOMContext(context.Background(), source, spec)
	if err != nil || calls != 1 {
		t.Fatal("initial context sampling failed")
	}
	frame, err := traceframe.NewSummaryVersion(traceframe.Summary{SessionEndedAt: time.Now().UTC(), Termination: trace.Cancelled, Incomplete: true}, traceframe.OOMVersion)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := state.finish(context.Background(), frame)
	if err != nil || calls != 2 {
		t.Fatal("terminal context sampling failed")
	}
	data, _ := traceframe.Encode(updated)
	if !strings.Contains(string(data), `"kubernetesContext":{"state":"observed"`) || !strings.Contains(string(data), `"pressureBefore":"false"`) {
		t.Fatal("sampled Kubernetes context was lost")
	}
}

func TestOOMControlRejectsStaleOrReplacedSamples(t *testing.T) {
	spec := oomContextSpec(t)
	source := oomContextFunc(func(_ context.Context, target trace.TargetIdentity) (trace.OOMKubernetesSample, error) {
		at := time.Now().Add(-time.Second).UTC()
		return trace.OOMKubernetesSample{Target: target, Start: at, End: at, NodePressure: "unreported"}, nil
	})
	if _, err := beginOOMContext(context.Background(), source, spec); !errors.Is(err, admission.ErrUnavailable) {
		t.Fatal("cached context was accepted as a fresh sample")
	}
	source = func(context.Context, trace.TargetIdentity) (trace.OOMKubernetesSample, error) {
		return trace.OOMKubernetesSample{}, admission.ErrTargetChanged
	}
	if _, err := beginOOMContext(context.Background(), source, spec); !errors.Is(err, admission.ErrTargetChanged) {
		t.Fatal("target replacement was hidden")
	}
}
