package admissionkube

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	admission "github.com/danushkastanley/kube-memlens/internal/traceadmission"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
)

func podFixture() *corev1.Pod {
	return &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: "tenant-a", Name: "pod", UID: types.UID("11111111-1111-1111-1111-111111111111")}, Spec: corev1.PodSpec{NodeName: "node-one", Containers: []corev1.Container{{Name: "worker"}}}, Status: corev1.PodStatus{Phase: corev1.PodRunning, QOSClass: corev1.PodQOSBurstable, ContainerStatuses: []corev1.ContainerStatus{{Name: "worker", ContainerID: "containerd://" + strings.Repeat("a", 64), State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{StartedAt: metav1.NewTime(time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC))}}}}}}
}
func nodeFixture() *corev1.Node {
	return &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-one", UID: types.UID("22222222-2222-2222-2222-222222222222")}}
}
func input(t *testing.T) admission.Request {
	t.Helper()
	r, err := admission.DecodeRequest("tenant-a", strings.NewReader(`{"schemaVersion":1,"pod":"pod","container":"worker","kind":"files"}`))
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestResolveUsesFullLiveLifetimeWithoutKernelIdentity(t *testing.T) {
	client := fake.NewSimpleClientset(podFixture(), nodeFixture())
	resolver := NewResolver(client.CoreV1())
	w, err := resolver.Resolve(context.Background(), input(t))
	if err != nil {
		t.Fatal(err)
	}
	if w.Target.ContainerID != strings.Repeat("a", 64) || w.Target.CgroupID != 0 || w.Target.PodUID != string(podFixture().UID) || w.Target.NodeUID != string(nodeFixture().UID) {
		t.Fatal("resolver lost or invented an identity")
	}
	if err := resolver.Revalidate(context.Background(), w); err != nil {
		t.Fatal(err)
	}
	for _, action := range client.Actions() {
		if action.GetVerb() != "get" {
			t.Fatal("resolver mutated or watched cluster state")
		}
	}
}

func TestRecreationRestartAndNodeReplacementInvalidateTheBinding(t *testing.T) {
	for _, scenario := range []string{"pod", "container-id", "container-start", "node"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := context.Background()
			pod, node := podFixture(), nodeFixture()
			client := fake.NewSimpleClientset(pod, node)
			resolver := NewResolver(client.CoreV1())
			original, err := resolver.Resolve(ctx, input(t))
			if err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "pod":
				if err := client.CoreV1().Pods(pod.Namespace).Delete(ctx, pod.Name, metav1.DeleteOptions{}); err != nil {
					t.Fatal(err)
				}
				pod.UID = "33333333-3333-3333-3333-333333333333"
				if _, err := client.CoreV1().Pods(pod.Namespace).Create(ctx, pod, metav1.CreateOptions{}); err != nil {
					t.Fatal(err)
				}
			case "container-id":
				pod.Status.ContainerStatuses[0].ContainerID = "containerd://" + strings.Repeat("b", 64)
				if _, err := client.CoreV1().Pods(pod.Namespace).UpdateStatus(ctx, pod, metav1.UpdateOptions{}); err != nil {
					t.Fatal(err)
				}
			case "container-start":
				pod.Status.ContainerStatuses[0].State.Running.StartedAt = metav1.NewTime(time.Now())
				if _, err := client.CoreV1().Pods(pod.Namespace).UpdateStatus(ctx, pod, metav1.UpdateOptions{}); err != nil {
					t.Fatal(err)
				}
			case "node":
				if err := client.CoreV1().Nodes().Delete(ctx, node.Name, metav1.DeleteOptions{}); err != nil {
					t.Fatal(err)
				}
				node.UID = "33333333-3333-3333-3333-333333333333"
				if _, err := client.CoreV1().Nodes().Create(ctx, node, metav1.CreateOptions{}); err != nil {
					t.Fatal(err)
				}
			}
			if err := resolver.Revalidate(ctx, original); !errors.Is(err, admission.ErrTargetChanged) {
				t.Fatalf("replacement accepted: %v", err)
			}
		})
	}
}

func TestIncompleteAndAmbiguousContainerStatesFailClosed(t *testing.T) {
	cases := map[string]func(*corev1.Pod){
		"prefix":  func(p *corev1.Pod) { p.Status.ContainerStatuses[0].ContainerID = "containerd://abcdef" },
		"runtime": func(p *corev1.Pod) { p.Status.ContainerStatuses[0].ContainerID = "cri-o://" + strings.Repeat("a", 64) },
		"waiting": func(p *corev1.Pod) {
			p.Status.ContainerStatuses[0].State = corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{}}
		},
		"conflicting": func(p *corev1.Pod) {
			p.Status.ContainerStatuses[0].State.Terminated = &corev1.ContainerStateTerminated{}
		},
		"duplicate": func(p *corev1.Pod) {
			p.Status.ContainerStatuses = append(p.Status.ContainerStatuses, p.Status.ContainerStatuses[0])
		},
		"wrong-category": func(p *corev1.Pod) {
			p.Status.InitContainerStatuses = p.Status.ContainerStatuses
			p.Status.ContainerStatuses = nil
		},
		"deleted":       func(p *corev1.Pod) { p.DeletionTimestamp = &metav1.Time{Time: time.Now()} },
		"finished":      func(p *corev1.Pod) { p.Status.Phase = corev1.PodSucceeded },
		"missing-start": func(p *corev1.Pod) { p.Status.ContainerStatuses[0].State.Running.StartedAt = metav1.Time{} },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			p := podFixture()
			change(p)
			resolver := NewResolver(fake.NewSimpleClientset(p, nodeFixture()).CoreV1())
			if _, err := resolver.Resolve(context.Background(), input(t)); err == nil {
				t.Fatal("invalid lifetime accepted")
			}
		})
	}
}

func TestRunningInitAndEphemeralContainersAreResolvedByTheirOwnStatus(t *testing.T) {
	for _, category := range []string{"init", "ephemeral"} {
		t.Run(category, func(t *testing.T) {
			p := podFixture()
			if category == "init" {
				p.Spec.InitContainers = p.Spec.Containers
				p.Status.InitContainerStatuses = p.Status.ContainerStatuses
				p.Status.Phase = corev1.PodPending
			} else {
				p.Spec.EphemeralContainers = []corev1.EphemeralContainer{{EphemeralContainerCommon: corev1.EphemeralContainerCommon{Name: "worker"}}}
				p.Status.EphemeralContainerStatuses = p.Status.ContainerStatuses
			}
			p.Spec.Containers = nil
			p.Status.ContainerStatuses = nil
			resolver := NewResolver(fake.NewSimpleClientset(p, nodeFixture()).CoreV1())
			if _, err := resolver.Resolve(context.Background(), input(t)); err != nil {
				t.Fatal(err)
			}
		})
	}
}
