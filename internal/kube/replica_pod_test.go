package kube

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/replicabaseline"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestReplicaShapeUsesImmutableImageAndResourceLayout(t *testing.T) {
	f, _ := newReplicaFixture(t)
	original, err := replicaPod(f.pod)
	if err != nil || original.Shape == "" {
		t.Fatal(err)
	}
	for _, change := range []string{"runtime ID", "mutable tag", "env", "generation", "digest", "configured limit", "reported applied", "reported allocated", "missing status", "unknown digest", "restart"} {
		t.Run(change, func(t *testing.T) {
			pod := f.pod.DeepCopy()
			changed := false
			switch change {
			case "runtime ID":
				pod.Status.ContainerStatuses[0].ContainerID = "containerd://replacement"
			case "mutable tag":
				pod.Spec.Containers[0].Image = "other/tag:latest"
			case "env":
				pod.Spec.Containers[0].Env = nil
			case "generation":
				pod.Generation++
			case "digest":
				pod.Status.ContainerStatuses[0].ImageID = "sha256:" + strings.Repeat("b", 64)
				changed = true
			case "configured limit":
				pod.Spec.Containers[0].Resources.Limits = corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("64Mi")}
				changed = true
			case "reported applied":
				pod.Status.ContainerStatuses[0].Resources = &corev1.ResourceRequirements{}
				pod.Status.ContainerStatuses[0].Resources.Limits = corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("64Mi")}
				changed = true
			case "reported allocated":
				pod.Status.ContainerStatuses[0].AllocatedResources = corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("16Mi")}
				changed = true
			case "missing status":
				pod.Status.ContainerStatuses = nil
				changed = true
			case "unknown digest":
				pod.Status.ContainerStatuses[0].ImageID = "image:tag"
				changed = true
			case "restart":
				pod.Status.ContainerStatuses[0].State.Running.StartedAt = metav1.NewTime(f.now.Add(-time.Minute))
			}
			member, err := replicaPod(*pod)
			if err != nil {
				t.Fatal(err)
			}
			if (member.Shape != original.Shape) != changed {
				t.Fatal("unexpected shape relationship")
			}
			if change == "restart" && !member.StableSince.Equal(f.now.Add(-time.Minute)) {
				t.Fatal("restart stability not reset")
			}
			if (change == "missing status" || change == "unknown digest") && member.Shape != "" {
				t.Fatal("incomplete shape reported")
			}
		})
	}
}

func TestReplicaPodRejectsMalformedContainerEvidence(t *testing.T) {
	for _, change := range []string{"duplicate spec", "duplicate status", "undeclared", "wrong role", "negative restarts", "backwards lifetime", "too many containers"} {
		t.Run(change, func(t *testing.T) {
			f, _ := newReplicaFixture(t)
			pod := f.pod.DeepCopy()
			expected := replicabaseline.ErrInvalid
			switch change {
			case "duplicate spec":
				pod.Spec.Containers = append(pod.Spec.Containers, pod.Spec.Containers[0])
			case "duplicate status":
				pod.Status.ContainerStatuses = append(pod.Status.ContainerStatuses, pod.Status.ContainerStatuses[0])
			case "undeclared":
				pod.Status.ContainerStatuses[0].Name = "hidden"
			case "wrong role":
				pod.Status.InitContainerStatuses = pod.Status.ContainerStatuses
				pod.Status.ContainerStatuses = nil
			case "negative restarts":
				pod.Status.ContainerStatuses[0].RestartCount = -1
			case "backwards lifetime":
				pod.Status.ContainerStatuses[0].State.Running.StartedAt = metav1.NewTime(pod.CreationTimestamp.Add(-time.Minute))
			case "too many containers":
				for len(pod.Spec.Containers) <= replicabaseline.MaxContainers {
					pod.Spec.Containers = append(pod.Spec.Containers, pod.Spec.Containers[0])
				}
				expected = replicabaseline.ErrBounds
			}
			if _, err := replicaPod(*pod); !errors.Is(err, expected) {
				t.Fatal(err)
			}
		})
	}
}

func TestReplicaPodPreservesExcludedLifecycles(t *testing.T) {
	f, _ := newReplicaFixture(t)
	for _, state := range []replicabaseline.Lifecycle{replicabaseline.Terminating, replicabaseline.Inactive, replicabaseline.Unready} {
		pod := f.pod.DeepCopy()
		switch state {
		case replicabaseline.Terminating:
			pod.DeletionTimestamp = &pod.CreationTimestamp
		case replicabaseline.Inactive:
			pod.Status.Phase = corev1.PodSucceeded
		case replicabaseline.Unready:
			pod.Status.Conditions = nil
		}
		m, err := replicaPod(*pod)
		if err != nil || m.Lifecycle != state {
			t.Fatalf("lifecycle %s: %s %v", state, m.Lifecycle, err)
		}
	}
}
