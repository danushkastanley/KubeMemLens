package admissionkube

import (
	"context"
	"errors"
	"testing"

	admission "github.com/danushkastanley/kube-memlens/internal/traceadmission"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestOOMContextUsesFreshTargetAndSeparateNodeCondition(t *testing.T) {
	pod, node := podFixture(), nodeFixture()
	pod.Status.ContainerStatuses[0].RestartCount = 3
	node.Status.Conditions = []corev1.NodeCondition{{Type: corev1.NodeMemoryPressure, Status: corev1.ConditionFalse}}
	client := fake.NewSimpleClientset(pod, node)
	r := NewResolver(client.CoreV1())
	workload, err := r.Resolve(context.Background(), input(t))
	if err != nil {
		t.Fatal(err)
	}
	target := workload.Target
	target.CgroupID = 123
	sample, err := r.SampleOOMContext(context.Background(), target)
	if err != nil || sample.Validate(target) != nil || sample.Restarts == nil || *sample.Restarts != 3 || sample.NodePressure != "false" {
		t.Fatal("Kubernetes context lost restart or explicit false state")
	}
	for _, action := range client.Actions() {
		if action.GetVerb() != "get" || (action.GetResource().Resource != "pods" && action.GetResource().Resource != "nodes") {
			t.Fatal("OOM context broadened the Kubernetes read surface")
		}
	}
	pod.UID = "replacement"
	if _, err := client.CoreV1().Pods(pod.Namespace).Update(context.Background(), pod, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.SampleOOMContext(context.Background(), target); !errors.Is(err, admission.ErrTargetChanged) {
		t.Fatal("OOM context retargeted a recreated Pod")
	}
}

func TestOptionalOOMConditionValidationDoesNotChangeAdmission(t *testing.T) {
	for _, status := range []corev1.ConditionStatus{corev1.ConditionTrue, corev1.ConditionFalse, corev1.ConditionUnknown, "invalid"} {
		node := nodeFixture()
		node.Status.Conditions = []corev1.NodeCondition{{Type: corev1.NodeMemoryPressure, Status: status}}
		client := fake.NewSimpleClientset(podFixture(), node)
		r := NewResolver(client.CoreV1())
		workload, err := r.Resolve(context.Background(), input(t))
		if err != nil {
			t.Fatal("optional context changed ordinary admission")
		}
		workload.Target.CgroupID = 123
		_, err = r.SampleOOMContext(context.Background(), workload.Target)
		if (status == "invalid") != (err != nil) {
			t.Fatal("OOM condition validity was not preserved")
		}
	}
	node := nodeFixture()
	if oomNodePressure(node) != "unreported" {
		t.Fatal("absent Node condition became false")
	}
	node.Status.Conditions = []corev1.NodeCondition{{Type: corev1.NodeMemoryPressure, Status: corev1.ConditionTrue}, {Type: corev1.NodeMemoryPressure, Status: corev1.ConditionFalse}}
	if oomNodePressure(node) != "" {
		t.Fatal("duplicate Node conditions were guessed")
	}
}
