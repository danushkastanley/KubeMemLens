package kube

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/collector"
	"github.com/danushkastanley/kube-memlens/internal/profiler"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
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
			requireAcceptedSnapshot(t, ref)
		})
	}
}

// A ReplicaSet's own owner reference is as unrestricted as a Pod's, and the
// agent reports it as the workload after the Pod's context has been built.
func TestOversizedResolvedWorkloadStillYieldsAnAcceptedSnapshot(t *testing.T) {
	controller := true
	for name, owner := range map[string]metav1.OwnerReference{
		"long workload kind": {Kind: strings.Repeat("K", 65), Name: "owner", Controller: &controller},
		"long workload name": {Kind: "Custom", Name: strings.Repeat("n", 254), Controller: &controller},
	} {
		t.Run(name, func(t *testing.T) {
			replicaSet := &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{
				Namespace: "default", Name: "api-rs", OwnerReferences: []metav1.OwnerReference{owner},
			}}
			pod := podWithStatuses("tenant-pod", "uid-a", []corev1.ContainerStatus{{Name: "app", ContainerID: "containerd://abcdef1234567890"}})
			pod.OwnerReferences = []metav1.OwnerReference{{Kind: "ReplicaSet", Name: "api-rs", Controller: &controller}}
			resolver := NewWorkloadOwnerResolver(fake.NewSimpleClientset(replicaSet), time.Minute)
			idx, failed := resolver.ResolveIndex(t.Context(), BuildPodIndexFromPods([]corev1.Pod{pod}), time.Unix(1_700_000_000, 0))
			if failed != 0 {
				t.Fatalf("owner lookup failed %d times", failed)
			}
			ref, ok := idx.Lookup("abcdef1234567890", "")
			if !ok {
				t.Fatal("Lookup returned false")
			}
			requireAcceptedSnapshot(t, ref)
		})
	}
}

func requireAcceptedSnapshot(t *testing.T, ref PodRef) {
	t.Helper()
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

// An operator is told to review memory-backed volumes that have no size limit.
// Fitting the count inside the collector's limit must not report them as limited.
func TestUnboundedMemoryVolumesStayVisibleOverTheVolumeLimit(t *testing.T) {
	pod := podWithStatuses("tenant-pod", "uid-a", []corev1.ContainerStatus{{Name: "app", ContainerID: "containerd://abcdef1234567890"}})
	limit := resource.MustParse("1Mi")
	for i := range 300 {
		source := &corev1.EmptyDirVolumeSource{Medium: corev1.StorageMediumMemory}
		if i < 260 {
			source.SizeLimit = &limit
		}
		pod.Spec.Volumes = append(pod.Spec.Volumes, corev1.Volume{
			Name: fmt.Sprintf("memory-%03d", i), VolumeSource: corev1.VolumeSource{EmptyDir: source},
		})
	}
	ref, ok := BuildPodIndexFromPods([]corev1.Pod{pod}).Lookup("abcdef1234567890", "")
	if !ok {
		t.Fatal("Lookup returned false")
	}
	requireAcceptedSnapshot(t, ref)
	if unbounded := ref.Context.MemoryEmptyDirCount - ref.Context.MemoryEmptyDirLimited; unbounded != 40 {
		t.Fatalf("300 volumes with 40 unbounded reported %d unbounded (count=%d limited=%d)",
			unbounded, ref.Context.MemoryEmptyDirCount, ref.Context.MemoryEmptyDirLimited)
	}
}

// The profiler handoff reads this label. A Pod with more labels than the
// collector accepts must not be reported as having declared no workflow.
func TestProfilingDeclarationSurvivesExcessLabels(t *testing.T) {
	pod := podWithStatuses("tenant-pod", "uid-a", []corev1.ContainerStatus{{Name: "app", ContainerID: "containerd://abcdef1234567890"}})
	pod.Labels = map[string]string{profiler.LabelPrefix + "app": profiler.GoHeapProfile}
	for i := range 70 {
		pod.Labels[fmt.Sprintf("label-%02d", i)] = "value"
	}
	ref, ok := BuildPodIndexFromPods([]corev1.Pod{pod}).Lookup("abcdef1234567890", "")
	if !ok {
		t.Fatal("Lookup returned false")
	}
	requireAcceptedSnapshot(t, ref)
	if got := ref.Context.Labels[profiler.LabelPrefix+"app"]; got != profiler.GoHeapProfile {
		t.Fatalf("profiling declaration was dropped from a Pod with 71 labels; got %q", got)
	}
}

// The snapshot cannot say that metadata was left out, so the agent has to
// count the Pods it reduced or an omitted owner reads as no owner.
func TestReducedPodsAreCounted(t *testing.T) {
	controller := true
	manyLabels := podWithStatuses("many-labels", "uid-a", []corev1.ContainerStatus{
		{Name: "app", ContainerID: "containerd://aaaaaaaaaaaaaaaa"}, {Name: "sidecar", ContainerID: "containerd://bbbbbbbbbbbbbbbb"},
	})
	manyLabels.Labels = map[string]string{}
	for i := range 65 {
		manyLabels.Labels[fmt.Sprintf("label-%02d", i)] = "value"
	}
	longWorkload := podWithStatuses("long-workload", "uid-b", []corev1.ContainerStatus{{Name: "app", ContainerID: "containerd://cccccccccccccccc"}})
	longWorkload.OwnerReferences = []metav1.OwnerReference{{Kind: "ReplicaSet", Name: "api-rs", Controller: &controller}}
	ordinary := podWithStatuses("ordinary", "uid-c", []corev1.ContainerStatus{{Name: "app", ContainerID: "containerd://dddddddddddddddd"}})
	ordinary.Labels = map[string]string{"app": "ordinary"}

	idx := BuildPodIndexFromPods([]corev1.Pod{manyLabels, longWorkload, ordinary})
	if got := idx.ReducedContextPods(); got != 1 {
		t.Fatalf("before owner resolution %d Pods counted as reduced, want 1 (the Pod with 65 labels)", got)
	}
	replicaSet := &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{
		Namespace: "default", Name: "api-rs",
		OwnerReferences: []metav1.OwnerReference{{Kind: "Custom", Name: strings.Repeat("n", 254), Controller: &controller}},
	}}
	idx, _ = NewWorkloadOwnerResolver(fake.NewSimpleClientset(replicaSet), time.Minute).ResolveIndex(t.Context(), idx, time.Unix(1_700_000_000, 0))
	if got := idx.ReducedContextPods(); got != 2 {
		t.Fatalf("after owner resolution %d Pods counted as reduced, want 2 (65 labels, and an omitted workload)", got)
	}
}
