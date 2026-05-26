package kube

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
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
