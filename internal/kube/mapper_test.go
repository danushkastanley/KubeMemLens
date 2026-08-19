package kube

import (
	"fmt"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestNormalizeContainerID(t *testing.T) {
	tests := map[string]string{
		"containerd://abcdef": "abcdef",
		"docker://ABCDEF":     "abcdef",
		"cri-o://abcdef":      "abcdef",
		"abcdef":              "abcdef",
	}

	for input, want := range tests {
		if got := NormalizeContainerID(input); got != want {
			t.Fatalf("NormalizeContainerID(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestContainerRuntime(t *testing.T) {
	for input, want := range map[string]string{
		"containerd://abc": "containerd",
		"docker://abc":     "docker",
		"cri-o://abc":      "cri-o",
		"crio://abc":       "cri-o",
		"abc":              "unknown",
	} {
		if got := ContainerRuntime(input); got != want {
			t.Fatalf("ContainerRuntime(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestBuildPodIndexAndExactLookup(t *testing.T) {
	idx := BuildPodIndexFromPods([]corev1.Pod{podWithStatuses("pod-a", "uid-a", []corev1.ContainerStatus{
		{Name: "app", ContainerID: "containerd://abcdef1234567890"},
	})})

	ref, ok := idx.Lookup("abcdef1234567890", "")
	if !ok {
		t.Fatal("Lookup returned false")
	}
	if ref.Namespace != "default" || ref.PodName != "pod-a" || ref.ContainerName != "app" {
		t.Fatalf("unexpected PodRef: %#v", ref)
	}
}

func TestBuildPodIndexIncludesWorkloadContext(t *testing.T) {
	controller := true
	runtimeClass := "gvisor"
	createdAt := metav1.NewTime(time.Unix(1_700_000_000, 0).UTC())
	finishedAt := metav1.NewTime(createdAt.Add(5 * time.Minute))
	pod := podWithStatuses("api-abc", "uid-a", []corev1.ContainerStatus{{
		Name:         "app",
		ContainerID:  "containerd://abcdef1234567890",
		RestartCount: 2,
		LastTerminationState: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{
			Reason:     "OOMKilled",
			ExitCode:   137,
			FinishedAt: finishedAt,
		}},
	}})
	pod.CreationTimestamp = createdAt
	pod.Labels = map[string]string{"app": "api"}
	pod.OwnerReferences = []metav1.OwnerReference{{Kind: "ReplicaSet", Name: "api-abc", Controller: &controller}}
	pod.Status.QOSClass = corev1.PodQOSBurstable
	pod.Status.Phase = corev1.PodRunning
	pod.Spec.Containers = []corev1.Container{{
		Name: "app",
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("128Mi")},
			Limits:   corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("256Mi")},
		},
	}}
	pod.Spec.RuntimeClassName = &runtimeClass
	pod.Spec.Volumes = []corev1.Volume{
		{Name: "bounded-cache", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{Medium: corev1.StorageMediumMemory, SizeLimit: quantityPointer(resource.MustParse("64Mi"))}}},
		{Name: "unbounded-cache", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{Medium: corev1.StorageMediumMemory}}},
		{Name: "disk", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
	}

	ref, ok := BuildPodIndexFromPods([]corev1.Pod{pod}).Lookup("abcdef1234567890", "")
	if !ok {
		t.Fatal("Lookup returned false")
	}
	context := ref.Context
	if !context.MemoryRequestKnown || context.MemoryRequestBytes != 128*1024*1024 {
		t.Fatalf("unexpected request context: %#v", context)
	}
	if !context.MemoryLimitKnown || context.MemoryLimitBytes != 256*1024*1024 {
		t.Fatalf("unexpected limit context: %#v", context)
	}
	if context.QoSClass != "Burstable" || context.RestartCount != 2 || context.PodPhase != "Running" {
		t.Fatalf("unexpected Pod status context: %#v", context)
	}
	if !context.LastTerminationKnown || context.LastTerminationReason != "OOMKilled" || context.LastTerminationExitCode != 137 {
		t.Fatalf("unexpected termination context: %#v", context)
	}
	if context.OwnerKind != "ReplicaSet" || context.OwnerName != "api-abc" {
		t.Fatalf("unexpected owner context: %#v", context)
	}
	if context.Labels["app"] != "api" {
		t.Fatalf("unexpected labels: %#v", context.Labels)
	}
	if context.RuntimeClassName != "gvisor" || context.MemoryEmptyDirCount != 2 || context.MemoryEmptyDirLimited != 1 || context.MemoryEmptyDirLimitBytes != 64*1024*1024 {
		t.Fatalf("unexpected runtime/emptyDir context: %#v", context)
	}
}

func quantityPointer(value resource.Quantity) *resource.Quantity {
	return &value
}

func TestLookupPrefix(t *testing.T) {
	idx := BuildPodIndexFromPods([]corev1.Pod{podWithStatuses("pod-a", "uid-a", []corev1.ContainerStatus{
		{Name: "app", ContainerID: "containerd://abcdef1234567890abcdef"},
	})})

	ref, ok := idx.Lookup("abcdef123456", "")
	if !ok {
		t.Fatal("Lookup returned false")
	}
	if ref.ContainerName != "app" {
		t.Fatalf("ContainerName = %q, want app", ref.ContainerName)
	}
}

func TestLookupShortPrefixReturnsFalse(t *testing.T) {
	idx := BuildPodIndexFromPods([]corev1.Pod{podWithStatuses("pod-a", "uid-a", []corev1.ContainerStatus{
		{Name: "app", ContainerID: "containerd://abcdef1234567890abcdef"},
	})})

	if _, ok := idx.Lookup("abcdef", ""); ok {
		t.Fatal("Lookup returned true for short prefix")
	}
}

func TestLookupAmbiguousPrefixReturnsFalse(t *testing.T) {
	idx := BuildPodIndexFromPods([]corev1.Pod{
		podWithStatuses("pod-a", "uid-a", []corev1.ContainerStatus{
			{Name: "app", ContainerID: "containerd://abcdef1234560000"},
		}),
		podWithStatuses("pod-b", "uid-b", []corev1.ContainerStatus{
			{Name: "app", ContainerID: "containerd://abcdef1234569999"},
		}),
	})

	if _, ok := idx.Lookup("abcdef123456", ""); ok {
		t.Fatal("Lookup returned true for ambiguous prefix")
	}
}

func TestLookupPodUIDFallbackOnlyWhenSafe(t *testing.T) {
	idx := BuildPodIndexFromPods([]corev1.Pod{
		podWithStatuses("pod-a", "uid-a", []corev1.ContainerStatus{
			{Name: "app", ContainerID: "containerd://abcdef1234567890"},
		}),
		podWithStatuses("pod-b", "uid-b", []corev1.ContainerStatus{
			{Name: "app", ContainerID: "containerd://bbbbbb1234567890"},
			{Name: "sidecar", ContainerID: "containerd://cccccc1234567890"},
		}),
	})

	if ref, ok := idx.Lookup("", "uid-a"); !ok || ref.PodName != "pod-a" {
		t.Fatalf("safe UID fallback failed: %#v ok=%v", ref, ok)
	}
	if _, ok := idx.Lookup("", "uid-b"); ok {
		t.Fatal("ambiguous UID fallback returned true")
	}
	if _, ok := idx.Lookup("unknown123456", "uid-a"); ok {
		t.Fatal("unmatched container ID should not fall back to pod UID")
	}
}

func BenchmarkBuildPodIndexAndLookup(b *testing.B) {
	for _, podCount := range []int{100, 1000, 5000, 10000} {
		b.Run(fmt.Sprintf("pods-%d", podCount), func(b *testing.B) {
			pods := make([]corev1.Pod, 0, podCount)
			containerIDs := make([]string, 0, podCount)
			for i := 0; i < podCount; i++ {
				containerID := fmt.Sprintf("%064x", i+1)
				containerIDs = append(containerIDs, containerID)
				pods = append(pods, podWithStatuses(
					fmt.Sprintf("pod-%d", i),
					fmt.Sprintf("uid-%d", i),
					[]corev1.ContainerStatus{{Name: "app", ContainerID: "containerd://" + containerID}},
				))
			}
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				index := BuildPodIndexFromPods(pods)
				for _, containerID := range containerIDs {
					if _, ok := index.Lookup(containerID, ""); !ok {
						b.Fatalf("Lookup(%s) returned false", containerID)
					}
				}
			}
		})
	}
}

func podWithStatuses(name string, uid string, statuses []corev1.ContainerStatus) corev1.Pod {
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      name,
			UID:       types.UID(uid),
		},
		Spec: corev1.PodSpec{NodeName: "node-a"},
		Status: corev1.PodStatus{
			ContainerStatuses: statuses,
		},
	}
}
