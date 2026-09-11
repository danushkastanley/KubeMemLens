package agentless

import (
	"math"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func TestPodMetadataReusesResourceContextWithoutRuntimeIDs(t *testing.T) {
	pod := pendingPod("team-a", "app")
	pod.Labels = map[string]string{"app": "example"}
	pod.Spec.Containers[0].Resources = corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("64Mi")}, Limits: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("128Mi"), "hugepages-2Mi": resource.MustParse("4Mi")}}
	pod.Spec.InitContainers = []corev1.Container{{Name: "prepare"}}
	pod.Spec.EphemeralContainers = []corev1.EphemeralContainer{{EphemeralContainerCommon: corev1.EphemeralContainerCommon{Name: "debug"}}}
	got, err := podMetadata(t.Context(), pod, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Containers) != 3 || got.Context.CreatedAt != pod.CreationTimestamp.Time || got.Context.MemoryRequestBytes != 64<<20 || got.Context.MemoryLimitBytes != 128<<20 {
		t.Fatalf("%+v", got)
	}
	if got.Containers[0].Context.Labels != nil || got.Context.Labels["app"] != "example" {
		t.Fatal("labels were duplicated or lost")
	}
	if len(got.Containers[0].Hugepages) != 1 || *got.Containers[0].Hugepages[0].LimitBytes != 4<<20 {
		t.Fatal("hugepage resources lost")
	}
}

func TestMetadataRejectsNumericOverflow(t *testing.T) {
	pod := pendingPod("team-a", "app")
	pod.Spec.Containers[0].Resources.Requests = corev1.ResourceList{corev1.ResourceMemory: *resource.NewQuantity(math.MaxInt64, resource.DecimalSI)}
	pod.Spec.Containers = append(pod.Spec.Containers, corev1.Container{Name: "second", Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("1")}}})
	if _, err := podMetadata(t.Context(), pod, time.Now()); err == nil {
		t.Fatal("resource sum overflow accepted")
	}
}

func TestMetadataReportsCurrentTerminationAndSanitisesControlText(t *testing.T) {
	pod := pendingPod("team-a", "app")
	pod.Status.ContainerStatuses = []corev1.ContainerStatus{{Name: "worker", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 137, Reason: "OOMKilled\x1b\u202e"}}}}
	got, err := podMetadata(t.Context(), pod, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	container := got.Containers[0]
	if container.State != "terminated" || container.StateReason != "OOMKilled" || container.ExitCode == nil || *container.ExitCode != 137 {
		t.Fatalf("%+v", container)
	}
	if strings.Contains(container.StateReason, "\x1b") {
		t.Fatal("terminal control text escaped")
	}
}
