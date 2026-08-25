package kube

import (
	"context"
	"errors"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
)

func TestNodeContextCacheReadsMemoryPressureAndCachesGet(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	transition := metav1.NewTime(now.Add(-time.Minute))
	client := fake.NewSimpleClientset(&corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "node-a", UID: "node-uid-a"},
		Status: corev1.NodeStatus{Allocatable: corev1.ResourceList{
			corev1.ResourceMemory: resource.MustParse("8Gi"),
		}, Conditions: []corev1.NodeCondition{{
			Type:               corev1.NodeMemoryPressure,
			Status:             corev1.ConditionFalse,
			LastTransitionTime: transition,
		}}},
	})
	cache := NewNodeContextCache(client, "node-a", time.Minute)

	first, err := cache.Context(context.Background(), now)
	if err != nil {
		t.Fatalf("Context returned error: %v", err)
	}
	second, err := cache.Context(context.Background(), now.Add(30*time.Second))
	if err != nil {
		t.Fatalf("cached Context returned error: %v", err)
	}
	if !first.Available || first.MemoryPressureStatus != "False" || !first.MemoryPressureSince.Equal(transition.Time) {
		t.Fatalf("unexpected node context: %#v", first)
	}
	if first.NodeUID != "node-uid-a" {
		t.Fatalf("NodeUID = %q, want node-uid-a", first.NodeUID)
	}
	if !first.MemoryAllocatableKnown || first.MemoryAllocatableBytes != 8*1024*1024*1024 {
		t.Fatalf("unexpected allocatable memory: %#v", first)
	}
	if second != first {
		t.Fatalf("cached context changed: first=%#v second=%#v", first, second)
	}
	if len(client.Actions()) != 1 {
		t.Fatalf("Kubernetes actions = %d, want one cached GET", len(client.Actions()))
	}
}

func TestNodeContextCacheRetainsOnlyNodeUIDAfterTransientError(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	client := fake.NewSimpleClientset(&corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "node-a", UID: "node-uid-a"},
		Status: corev1.NodeStatus{
			Allocatable: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("8Gi")},
			Conditions:  []corev1.NodeCondition{{Type: corev1.NodeMemoryPressure, Status: corev1.ConditionTrue}},
		},
	})
	cache := NewNodeContextCache(client, "node-a", time.Second)
	if _, err := cache.Context(context.Background(), now); err != nil {
		t.Fatalf("initial Context: %v", err)
	}

	transient := errors.New("temporary Kubernetes API outage")
	client.PrependReactor("get", "nodes", func(clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, transient
	})
	degraded, err := cache.Context(context.Background(), now.Add(2*time.Second))
	if !errors.Is(err, transient) {
		t.Fatalf("degraded Context error = %v, want transient error", err)
	}
	if degraded.NodeUID != "node-uid-a" {
		t.Fatalf("degraded NodeUID = %q, want last-known identity", degraded.NodeUID)
	}
	if degraded.Available || degraded.MemoryPressureStatus != "" || degraded.MemoryAllocatableKnown || degraded.MemoryAllocatableBytes != 0 {
		t.Fatalf("degraded Context retained stale operational data: %#v", degraded)
	}
}
