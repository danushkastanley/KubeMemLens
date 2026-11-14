package kube

import (
	"context"
	"errors"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
)

// failingNodeClient serves node-a, failing GETs while *failing is true.
func failingNodeClient(failing *bool, failure error) *fake.Clientset {
	client := fake.NewSimpleClientset(&corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "node-a", UID: "node-uid-a"},
	})
	client.PrependReactor("get", "nodes", func(clienttesting.Action) (bool, runtime.Object, error) {
		return *failing, nil, failure
	})
	return client
}

func TestNodeContextCacheRetriesSoonAfterAFailedGet(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	failing := true
	outage := errors.New("temporary Kubernetes API outage")
	client := failingNodeClient(&failing, outage)
	cache := NewNodeContextCache(client, "node-a", 30*time.Second)

	first, err := cache.Context(context.Background(), now)
	if !errors.Is(err, outage) || first.NodeUID != "" {
		t.Fatalf("first Context = %#v, %v; want the outage and no node UID", first, err)
	}

	failing = false
	recovered, err := cache.Context(context.Background(), now.Add(6*time.Second))
	if err != nil {
		t.Fatalf("Context 6s after a failed GET: %v", err)
	}
	if recovered.NodeUID != "node-uid-a" || !recovered.Available {
		t.Fatalf("recovered Context = %#v, want node-uid-a and available", recovered)
	}
}

func TestNodeContextCacheHoldsAFailureUntilTheRetryInterval(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	failing := true
	outage := errors.New("temporary Kubernetes API outage")
	client := failingNodeClient(&failing, outage)
	cache := NewNodeContextCache(client, "node-a", 30*time.Second)

	if _, err := cache.Context(context.Background(), now); !errors.Is(err, outage) {
		t.Fatalf("first Context error = %v, want the outage", err)
	}
	failing = false
	if _, err := cache.Context(context.Background(), now.Add(4*time.Second)); !errors.Is(err, outage) {
		t.Fatalf("Context 4s after a failed GET error = %v, want the held outage", err)
	}
	if got := len(client.Actions()); got != 1 {
		t.Fatalf("Kubernetes actions = %d, want one GET inside the retry interval", got)
	}
}

func TestNodeContextCacheKeepsASuccessForTheFullTTLAfterRecovery(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	failing := true
	client := failingNodeClient(&failing, errors.New("temporary Kubernetes API outage"))
	cache := NewNodeContextCache(client, "node-a", 30*time.Second)

	_, _ = cache.Context(context.Background(), now)
	failing = false
	recoveredAt := now.Add(6 * time.Second)
	if _, err := cache.Context(context.Background(), recoveredAt); err != nil {
		t.Fatalf("recovery Context: %v", err)
	}
	if _, err := cache.Context(context.Background(), recoveredAt.Add(29*time.Second)); err != nil {
		t.Fatalf("cached Context: %v", err)
	}
	if got := len(client.Actions()); got != 2 {
		t.Fatalf("Kubernetes actions = %d, want the failed GET and one successful GET", got)
	}
}
