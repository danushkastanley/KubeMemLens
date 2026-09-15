package trace

import (
	"errors"
	"fmt"
	"io"
	"time"
)

// OOMKubernetesSample comes from fresh authorised Pod/Node reads. Its cgroup
// identity is the admitted binding; Kubernetes does not measure cgroup IDs.
type OOMKubernetesSample struct {
	Target       TargetIdentity
	Start, End   time.Time
	Restarts     *uint64
	NodePressure string // true, false, unknown, unreported
}

func (OOMKubernetesSample) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "[ephemeral Kubernetes OOM sample]")
}
func (OOMKubernetesSample) MarshalJSON() ([]byte, error) {
	return nil, errors.New("Kubernetes evidence requires explicit encoding")
}

func (s OOMKubernetesSample) Validate(target TargetIdentity) error {
	if target.ValidateLifetime() != nil || target.CgroupID == 0 || s.Target != target || s.Start.IsZero() || s.Start.Before(target.ContainerStartedAt) || s.End.Before(s.Start) || s.End.Sub(s.Start) > time.Second || (s.Restarts != nil && *s.Restarts > 1<<31-1) || !validNodePressure(s.NodePressure) {
		return errors.New("invalid Kubernetes OOM sample")
	}
	return nil
}

// KubernetesOOMContext brackets the trace causally: the first sample finishes
// before node execution and the second starts after its stream ends. It does not
// claim cross-node clock alignment or classify the kernel OOM scope.
type KubernetesOOMContext struct {
	State                                        string // observed, unavailable, target_changed
	BeforeStart, BeforeEnd, AfterStart, AfterEnd time.Time
	Restarts                                     CounterDelta
	PressureBefore, PressureAfter                string
}

func (KubernetesOOMContext) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "[ephemeral Kubernetes OOM context]")
}
func (KubernetesOOMContext) MarshalJSON() ([]byte, error) {
	return nil, errors.New("Kubernetes evidence requires explicit encoding")
}

func (c KubernetesOOMContext) Validate(duration time.Duration) error {
	invalid := errors.New("invalid Kubernetes OOM context")
	if c.State != "observed" {
		if c.State != "unavailable" && c.State != "target_changed" {
			return invalid
		}
		c.State = ""
		if c != (KubernetesOOMContext{}) {
			return invalid
		}
		return nil
	}
	if duration <= 0 || duration > 5*time.Minute || c.BeforeStart.IsZero() || c.BeforeEnd.Before(c.BeforeStart) || c.BeforeEnd.Sub(c.BeforeStart) > time.Second || !c.AfterStart.After(c.BeforeEnd) || c.AfterEnd.Before(c.AfterStart) || c.AfterEnd.Sub(c.AfterStart) > time.Second || c.AfterEnd.Sub(c.BeforeStart) > duration+2*time.Second || !validCounterDelta(c.Restarts) || !validNodePressure(c.PressureBefore) || !validNodePressure(c.PressureAfter) {
		return invalid
	}
	if c.Restarts.Delta != nil && *c.Restarts.Delta > 1<<31-1 {
		return invalid
	}
	return nil
}

func CorrelateKubernetesOOM(spec Specification, before, after OOMKubernetesSample) KubernetesOOMContext {
	if spec.Validate() != nil || spec.Kind() != OOM {
		return KubernetesOOMContext{State: "unavailable"}
	}
	if before.Target != spec.Target() || after.Target != spec.Target() {
		return KubernetesOOMContext{State: "target_changed"}
	}
	if before.Validate(spec.Target()) != nil || after.Validate(spec.Target()) != nil {
		return KubernetesOOMContext{State: "unavailable"}
	}
	delta := CounterDelta{State: "unreported"}
	if before.Restarts != nil && after.Restarts != nil {
		delta.State = "reset"
		if *after.Restarts >= *before.Restarts {
			value := *after.Restarts - *before.Restarts
			delta = CounterDelta{State: "reported", Delta: &value}
		}
	}
	result := KubernetesOOMContext{State: "observed", BeforeStart: before.Start, BeforeEnd: before.End, AfterStart: after.Start, AfterEnd: after.End, Restarts: delta, PressureBefore: before.NodePressure, PressureAfter: after.NodePressure}
	if result.Validate(spec.Bounds().Duration) != nil {
		return KubernetesOOMContext{State: "unavailable"}
	}
	return result
}

func validNodePressure(state string) bool {
	return state == "true" || state == "false" || state == "unknown" || state == "unreported"
}
