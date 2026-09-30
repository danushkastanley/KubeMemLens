package installcheck

import (
	"context"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type NodeReader interface {
	Get(context.Context, string, metav1.GetOptions) (*corev1.Node, error)
}

// CheckNodes reads current API state; retained metadata cannot establish a new
// installation. Runtime preflight still checks the actual mounted host/kernel.
func CheckNodes(ctx context.Context, reader NodeReader, spec Spec) error {
	if ctx == nil || reader == nil || spec.Validate() != nil {
		return ErrConfiguration
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if ctx.Err() != nil {
		return ErrUnavailable
	}
	for _, selected := range spec.Nodes {
		node, err := reader.Get(ctx, selected.Name, metav1.GetOptions{})
		if err != nil || node == nil || ctx.Err() != nil {
			return ErrUnavailable
		}
		if !selected.profile().Matches(node) || node.Spec.Unschedulable {
			return ErrMismatch
		}
		ready := 0
		for _, condition := range node.Status.Conditions {
			if condition.Type != corev1.NodeReady {
				continue
			}
			if condition.Status != corev1.ConditionTrue {
				return ErrUnavailable
			}
			ready++
		}
		if ready != 1 {
			return ErrUnavailable
		}
	}
	return nil
}
