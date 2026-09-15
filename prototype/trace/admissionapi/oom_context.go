package admissionapi

import (
	"context"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	admission "github.com/danushkastanley/kube-memlens/internal/traceadmission"
	"github.com/danushkastanley/kube-memlens/internal/traceframe"
)

type OOMContextSource interface {
	SampleOOMContext(context.Context, trace.TargetIdentity) (trace.OOMKubernetesSample, error)
}

// WithOOMContext returns a new proxy configuration; live configuration is not
// mutated. The source is selected by installation, never by a request.
func (p *StreamProxy) WithOOMContext(source OOMContextSource) *StreamProxy {
	copy := *p
	copy.oomContext = source
	return &copy
}

type oomSessionContext struct {
	source OOMContextSource
	spec   trace.Specification
	before trace.OOMKubernetesSample
}

func beginOOMContext(ctx context.Context, source OOMContextSource, spec trace.Specification) (*oomSessionContext, error) {
	result := &oomSessionContext{source: source, spec: spec}
	if source == nil {
		return result, nil
	}
	var err error
	result.before, err = sampleOOMContext(ctx, source, spec.Target())
	return result, err
}

func sampleOOMContext(parent context.Context, source OOMContextSource, target trace.TargetIdentity) (trace.OOMKubernetesSample, error) {
	ctx, cancel := context.WithTimeout(parent, time.Second)
	defer cancel()
	started := time.Now().UTC()
	sample, err := source.SampleOOMContext(ctx, target)
	ended := time.Now().UTC()
	if err != nil {
		return trace.OOMKubernetesSample{}, err
	}
	if ctx.Err() != nil || sample.Validate(target) != nil || sample.Start.Before(started) || sample.End.After(ended) {
		return trace.OOMKubernetesSample{}, admission.ErrUnavailable
	}
	return sample, nil
}

func (c *oomSessionContext) finish(ctx context.Context, frame traceframe.Frame) (traceframe.Frame, error) {
	evidence := trace.KubernetesOOMContext{State: "unavailable"}
	if c.source != nil {
		after, err := sampleOOMContext(ctx, c.source, c.spec.Target())
		if err != nil {
			return traceframe.Frame{}, err
		}
		evidence = trace.CorrelateKubernetesOOM(c.spec, c.before, after)
	}
	updated, err := frame.WithOOMKubernetesContext(&evidence, time.Now().UTC(), c.spec.Bounds().Duration)
	if err != nil {
		return traceframe.Frame{}, admission.ErrUnavailable
	}
	return updated, nil
}
