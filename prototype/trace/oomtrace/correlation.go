package oomtrace

import (
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	"github.com/danushkastanley/kube-memlens/prototype/trace/filecache"
)

func unavailable(state string) trace.OOMCorrelation {
	return trace.OOMCorrelation{Window: trace.CorrelationWindow{State: state}}
}

// Correlate preserves separate local/hierarchical counters and the full sampling
// interval. Deltas are not scaled into a kill count or a causal explanation.
func Correlate(spec trace.Specification, window filecache.ObservationWindow, before, after Sample) trace.OOMCorrelation {
	if spec.Validate() != nil || spec.Kind() != trace.OOM || before.start.IsZero() || after.start.IsZero() || !after.start.After(before.end) || after.end.Sub(before.start) > spec.Bounds().Duration+2*time.Second {
		return unavailable("unavailable")
	}
	if spec.Target() != before.target || spec.Target() != after.target {
		return unavailable("target_changed")
	}
	if window.Start.IsZero() || window.Start.Before(spec.Target().ContainerStartedAt) || !window.End.After(window.Start) || window.End.Sub(window.Start) > spec.Bounds().Duration || window.Uncertainty == nil || *window.Uncertainty < 0 || *window.Uncertainty > 5*time.Millisecond {
		return unavailable("clock_uncertain")
	}
	start, end := window.Start.Add(*window.Uncertainty), window.End.Add(-*window.Uncertainty)
	if before.end.After(start) {
		start = before.end
	}
	if after.start.Before(end) {
		end = after.start
	}
	if !end.After(start) {
		return unavailable("disjoint")
	}
	uncertainty := *window.Uncertainty
	result := trace.OOMCorrelation{
		Window: trace.CorrelationWindow{State: "overlapping", EvidenceStart: before.start, BeforeEnd: before.end, AfterStart: after.start, EvidenceEnd: after.end, OverlapStart: start, OverlapEnd: end, Uncertainty: &uncertainty},
		Local:  eventDeltas(before.local, after.local), Hierarchical: eventDeltas(before.hierarchical, after.hierarchical),
		Current:     trace.GaugePair{Before: clone(before.current), After: clone(after.current)},
		LimitBefore: cloneLimit(before.limit), LimitAfter: cloneLimit(after.limit),
		PSISome: delta(before.some, after.some), PSIFull: delta(before.full, after.full),
	}
	if result.Validate(window.Start, window.End, spec.Bounds().Duration) != nil {
		return unavailable("unavailable")
	}
	return result
}

func eventDeltas(before, after map[string]uint64) trace.OOMEventDeltas {
	values := make([]trace.CounterDelta, 0, 6)
	for _, key := range []string{"low", "high", "max", "oom", "oom_kill", "oom_group_kill"} {
		values = append(values, delta(value(before, key), value(after, key)))
	}
	return trace.OOMEventDeltas{Low: values[0], High: values[1], Max: values[2], OOM: values[3], Kill: values[4], GroupKill: values[5]}
}

func value(values map[string]uint64, key string) *uint64 {
	value, ok := values[key]
	if !ok {
		return nil
	}
	return &value
}

func delta(before, after *uint64) trace.CounterDelta {
	if before == nil || after == nil {
		return trace.CounterDelta{State: "unreported"}
	}
	if *after < *before {
		return trace.CounterDelta{State: "reset"}
	}
	value := *after - *before
	return trace.CounterDelta{State: "reported", Delta: &value}
}

func clone(value *uint64) *uint64 {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func cloneLimit(limit trace.OOMLimit) trace.OOMLimit {
	return trace.OOMLimit{State: limit.State, Bytes: clone(limit.Bytes)}
}
