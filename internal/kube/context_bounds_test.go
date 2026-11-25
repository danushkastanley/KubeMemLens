package kube

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/collector"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// The collector rejects a whole node snapshot when one container's context is
// over a limit. Every Pod below is one the API server accepts, so none of them
// may stop its node from publishing.
func TestOversizedPodMetadataStillYieldsAnAcceptedSnapshot(t *testing.T) {
	controller := true
	for name, change := range map[string]func(*corev1.Pod){
		"more labels than the collector accepts": func(pod *corev1.Pod) {
			pod.Labels = map[string]string{}
			for i := range 65 {
				pod.Labels[fmt.Sprintf("label-%02d", i)] = "value"
			}
		},
		"label key at the Kubernetes maximum": func(pod *corev1.Pod) {
			pod.Labels = map[string]string{strings.Repeat("a", 253) + "/" + strings.Repeat("b", 63): "value"}
		},
		"long owner kind": func(pod *corev1.Pod) {
			pod.OwnerReferences = []metav1.OwnerReference{{Kind: strings.Repeat("K", 65), Name: "owner", Controller: &controller}}
		},
		"long owner name": func(pod *corev1.Pod) {
			pod.OwnerReferences = []metav1.OwnerReference{{Kind: "Custom", Name: strings.Repeat("n", 254), Controller: &controller}}
		},
		"long runtime class name": func(pod *corev1.Pod) {
			runtimeClass := strings.Repeat("r", 65)
			pod.Spec.RuntimeClassName = &runtimeClass
		},
		"long termination reason": func(pod *corev1.Pod) {
			pod.Status.ContainerStatuses[0].LastTerminationState.Terminated = &corev1.ContainerStateTerminated{Reason: strings.Repeat("x", 65)}
		},
		"more memory-backed emptyDir volumes than the collector accepts": func(pod *corev1.Pod) {
			for i := range 257 {
				pod.Spec.Volumes = append(pod.Spec.Volumes, corev1.Volume{
					Name:         fmt.Sprintf("memory-%03d", i),
					VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{Medium: corev1.StorageMediumMemory}},
				})
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			pod := podWithStatuses("tenant-pod", "uid-a", []corev1.ContainerStatus{{Name: "app", ContainerID: "containerd://abcdef1234567890"}})
			change(&pod)
			ref, ok := BuildPodIndexFromPods([]corev1.Pod{pod}).Lookup("abcdef1234567890", "")
			if !ok {
				t.Fatal("Lookup returned false")
			}
			now := time.Unix(1_700_000_000, 0).UTC()
			snapshot := api.AgentSnapshot{
				SchemaVersion: api.CurrentSnapshotSchemaVersion, NodeName: ref.NodeName, CapturedAt: now,
				Containers: []api.ContainerSnapshot{{
					Namespace: ref.Namespace, PodName: ref.PodName, PodUID: ref.PodUID,
					ContainerName: ref.ContainerName, ContainerID: ref.ContainerID, Context: ref.Context,
				}},
			}
			if err := collector.ValidateSnapshot(snapshot, now, collector.DefaultHandlerOptions(time.Minute)); err != nil {
				t.Fatalf("collector would reject the whole node snapshot for this Pod: %v", err)
			}
		})
	}
}

func TestExcessLabelsAreDroppedInKeyOrder(t *testing.T) {
	pod := podWithStatuses("tenant-pod", "uid-a", []corev1.ContainerStatus{{Name: "app", ContainerID: "containerd://abcdef1234567890"}})
	pod.Labels = map[string]string{strings.Repeat("k", 254): "over-length key is dropped first"}
	for i := range 70 {
		pod.Labels[fmt.Sprintf("label-%02d", i)] = "value"
	}
	for range 5 {
		ref, _ := BuildPodIndexFromPods([]corev1.Pod{pod}).Lookup("abcdef1234567890", "")
		if len(ref.Context.Labels) != 64 {
			t.Fatalf("got %d labels, want 64", len(ref.Context.Labels))
		}
		for _, key := range []string{"label-00", "label-63"} {
			if ref.Context.Labels[key] != "value" {
				t.Fatalf("label %s was dropped; the first 64 keys in order must be kept", key)
			}
		}
		if _, kept := ref.Context.Labels["label-64"]; kept {
			t.Fatal("label-64 was kept; selection must not depend on map order")
		}
	}
}
