package kube

import (
	"context"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/model"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes/fake"
)

func resourcePod() corev1.Pod {
	pod := cachePod("app", "uid-app", "node-a", "id-app")
	pod.Generation = 3
	pod.Spec.Containers = []corev1.Container{{Name: "app", Resources: resourceBudget("128Mi", "256Mi")}}
	pod.Status.ObservedGeneration = 2
	return *pod
}

func resourceBudget(request, limit string) corev1.ResourceRequirements {
	budget := corev1.ResourceRequirements{}
	if request != "" {
		budget.Requests = corev1.ResourceList{corev1.ResourceMemory: resource.MustParse(request)}
	}
	if limit != "" {
		budget.Limits = corev1.ResourceList{corev1.ResourceMemory: resource.MustParse(limit)}
	}
	return budget
}

func TestContainerOnlyResourcesRetainLegacyContext(t *testing.T) {
	pod := resourcePod()
	status := pod.Status.ContainerStatuses[0]
	applied := resourceBudget("128Mi", "256Mi")
	status.Resources = &applied
	status.AllocatedResources = applied.Requests
	if got := memoryResourceContext(pod, status); !got.IsZero() {
		t.Fatalf("ordinary container resources gained an extension: %+v", got)
	}
	status.Name = "unmapped-container"
	if got := memoryResourceContext(pod, status); !got.IsZero() {
		t.Fatalf("missing container spec was treated as a resize: %+v", got)
	}
}

func TestPodResourceContextSeparatesConfiguredAllocatedAndApplied(t *testing.T) {
	pod := resourcePod()
	configured, applied := resourceBudget("192Mi", "384Mi"), resourceBudget("128Mi", "256Mi")
	pod.Spec.Resources, pod.Status.Resources = &configured, &applied
	pod.Status.AllocatedResources = configured.Requests
	pod.Status.ContainerStatuses[0].Resources = &applied
	ref, ok := BuildPodIndexFromPods([]corev1.Pod{pod}).Lookup("id-app", "")
	if !ok {
		t.Fatal("Pod mapping failed")
	}
	got := ref.Context.Resources
	if got.Pod.Configured.Request.Bytes != 192<<20 || got.Pod.Configured.Limit.Bytes != 384<<20 ||
		got.Pod.AllocatedRequest.Bytes != 192<<20 || got.Pod.Applied.Limit.Bytes != 256<<20 ||
		got.Applied.Request.Bytes != 128<<20 || got.Pod.Generation != 3 || got.Pod.ObservedGeneration != 2 {
		t.Fatalf("resource scopes or generations changed: %+v", got)
	}
	if ref.Context.MemoryRequestBytes != 128<<20 || ref.Context.MemoryLimitBytes != 256<<20 {
		t.Fatal("Pod budget replaced the container contribution")
	}
	if err := got.Validate(); err != nil {
		t.Fatal(err)
	}
	configured.Limits[corev1.ResourceMemory] = resource.MustParse("512Mi")
	if got.Pod.Configured.Limit.Bytes != 384<<20 {
		t.Fatal("mapped resource context shares mutable Kubernetes quantities")
	}
}

func TestRemovedContainerLimitRetainsOldAppliedValueDuringResize(t *testing.T) {
	pod := resourcePod()
	pod.Spec.Containers[0].Resources = resourceBudget("128Mi", "")
	applied := resourceBudget("128Mi", "256Mi")
	pod.Status.ContainerStatuses[0].Resources = &applied
	got := memoryResourceContext(pod, pod.Status.ContainerStatuses[0])
	if got.IsZero() || !got.Applied.Limit.Known || got.Applied.Limit.Bytes != 256<<20 {
		t.Fatalf("previous applied limit disappeared: %+v", got)
	}
}

func TestResizeConditionsPreserveStatesAndPrecedence(t *testing.T) {
	for _, tc := range []struct {
		kind   corev1.PodConditionType
		status corev1.ConditionStatus
		reason string
		want   model.ResizeState
	}{
		{corev1.PodResizePending, corev1.ConditionTrue, "", model.ResizePending},
		{corev1.PodResizePending, corev1.ConditionTrue, corev1.PodReasonDeferred, model.ResizeDeferred},
		{corev1.PodResizePending, corev1.ConditionTrue, corev1.PodReasonInfeasible, model.ResizeInfeasible},
		{corev1.PodResizePending, corev1.ConditionTrue, "FutureReason", model.ResizeUnknown},
		{corev1.PodResizePending, corev1.ConditionUnknown, "", model.ResizeUnknown},
		{corev1.PodResizePending, corev1.ConditionFalse, "", model.ResizeNone},
		{corev1.PodResizeInProgress, corev1.ConditionTrue, "", model.ResizeApplying},
		{corev1.PodResizeInProgress, corev1.ConditionTrue, corev1.PodReasonError, model.ResizeError},
		{corev1.PodResizeInProgress, corev1.ConditionTrue, "FutureReason", model.ResizeUnknown},
	} {
		t.Run(string(tc.kind)+"/"+string(tc.status)+"/"+tc.reason, func(t *testing.T) {
			status := corev1.PodStatus{Resize: corev1.PodResizeStatusDeferred, Conditions: []corev1.PodCondition{{
				Type: tc.kind, Status: tc.status, Reason: tc.reason, ObservedGeneration: 3,
				LastTransitionTime: metav1.NewTime(time.Unix(100, 0)),
				Message:            "private status text must not enter memory context",
			}}}
			pending, applying := podResizeObservations(status)
			got := pending
			if tc.kind == corev1.PodResizeInProgress {
				got = applying
				if pending.State != model.ResizeNone {
					t.Fatal("deprecated resize state overrode modern conditions")
				}
			}
			if got.State != tc.want {
				t.Fatalf("resize state=%s, want %s", got.State, tc.want)
			}
			resources := model.ContainerMemoryResources{Pod: model.PodMemoryResources{Pending: pending, Applying: applying}}
			if err := resources.Validate(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestLegacyResizeAndConcurrentGenerations(t *testing.T) {
	for legacy, want := range map[corev1.PodResizeStatus]model.ResizeState{
		corev1.PodResizeStatusInProgress: model.ResizeApplying,
		corev1.PodResizeStatusDeferred:   model.ResizeDeferred,
		corev1.PodResizeStatusInfeasible: model.ResizeInfeasible,
		"FutureState":                    model.ResizeUnknown,
	} {
		pending, applying := podResizeObservations(corev1.PodStatus{Resize: legacy})
		got := pending
		if legacy == corev1.PodResizeStatusInProgress {
			got = applying
		}
		if got.State != want || got.Source != model.ResizeLegacyStatus || !got.TransitionAt.IsZero() {
			t.Fatalf("legacy resize changed: %+v", got)
		}
	}
	pending, applying := podResizeObservations(corev1.PodStatus{Conditions: []corev1.PodCondition{
		{Type: corev1.PodResizePending, Status: corev1.ConditionTrue, Reason: corev1.PodReasonDeferred, ObservedGeneration: 4},
		{Type: corev1.PodResizeInProgress, Status: corev1.ConditionTrue, ObservedGeneration: 3},
	}})
	if pending.State != model.ResizeDeferred || pending.ObservedGeneration != 4 ||
		applying.State != model.ResizeApplying || applying.ObservedGeneration != 3 {
		t.Fatal("concurrent resize generations were collapsed")
	}
}

func TestPodResourceInformerUpdatesWithoutAgentRestart(t *testing.T) {
	pod := resourcePod()
	initial := resourceBudget("192Mi", "384Mi")
	pod.Spec.Resources = &initial
	client := fake.NewSimpleClientset(&pod)
	cache := NewPodCache(client, "node-a")
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	go cache.Run(ctx)
	if !cache.WaitForSync(ctx) {
		t.Fatal("Pod cache did not sync")
	}
	updated := pod.DeepCopy()
	updated.Generation = 4
	budget := resourceBudget("256Mi", "512Mi")
	updated.Spec.Resources = &budget
	updated.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodResizePending, Status: corev1.ConditionTrue, Reason: corev1.PodReasonDeferred, ObservedGeneration: 4}}
	if _, err := client.CoreV1().Pods(updated.Namespace).Update(ctx, updated, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := wait.PollUntilContextTimeout(ctx, 10*time.Millisecond, 5*time.Second, true, func(context.Context) (bool, error) {
		ref, ok := cache.Index().Lookup("id-app", "")
		return ok && ref.Context.Resources.Pod.Configured.Limit.Bytes == 512<<20 &&
			ref.Context.Resources.Pod.Generation == 4 && ref.Context.Resources.Pod.Pending.State == model.ResizeDeferred, nil
	}); err != nil {
		t.Fatalf("resize did not reach the current Pod index: %v", err)
	}
}
