package admissionkube

import (
	"context"
	"errors"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	admission "github.com/danushkastanley/kube-memlens/internal/traceadmission"
	corev1 "k8s.io/api/core/v1"
)

// SampleOOMContext uses the same fresh Pod/Node reads and identity validation as
// admission. It adds no Kubernetes privileges and exports no process data.
func (r *Resolver) SampleOOMContext(ctx context.Context, target trace.TargetIdentity) (trace.OOMKubernetesSample, error) {
	started := time.Now().UTC()
	snapshot, err := r.readWorkload(ctx, target.Namespace, target.PodName, target.ContainerName)
	ended := time.Now().UTC()
	if errors.Is(err, admission.ErrNotFound) {
		return trace.OOMKubernetesSample{}, admission.ErrTargetChanged
	}
	if err != nil {
		return trace.OOMKubernetesSample{}, err
	}
	expected := target
	expected.CgroupID = 0 // Kubernetes cannot measure the node's cgroup binding.
	if !sameTarget(snapshot.workload.Target, expected) {
		return trace.OOMKubernetesSample{}, admission.ErrTargetChanged
	}
	if snapshot.restarts < 0 || ctx.Err() != nil {
		return trace.OOMKubernetesSample{}, admission.ErrUnavailable
	}
	restarts := uint64(snapshot.restarts)
	sample := trace.OOMKubernetesSample{Target: target, Start: started, End: ended, Restarts: &restarts, NodePressure: snapshot.nodePressure}
	if sample.Validate(target) != nil {
		return trace.OOMKubernetesSample{}, admission.ErrUnavailable
	}
	return sample, nil
}

// Empty means an invalid condition set; unreported means the condition is absent.
// Ordinary admission ignores this optional evidence and retains its old rules.
func oomNodePressure(node *corev1.Node) string {
	if len(node.Status.Conditions) > 64 {
		return ""
	}
	value := "unreported"
	for _, condition := range node.Status.Conditions {
		if condition.Type != corev1.NodeMemoryPressure {
			continue
		}
		if value != "unreported" {
			return ""
		}
		switch condition.Status {
		case corev1.ConditionTrue:
			value = "true"
		case corev1.ConditionFalse:
			value = "false"
		case corev1.ConditionUnknown:
			value = "unknown"
		default:
			return ""
		}
	}
	return value
}
