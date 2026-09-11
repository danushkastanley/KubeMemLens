package kube

import (
	"errors"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestOwnerReferenceDoesNotJoinARecreatedController(t *testing.T) {
	client := fake.NewSimpleClientset(&appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "replica", UID: "new-uid", OwnerReferences: []metav1.OwnerReference{{Kind: "Deployment", Name: "new-owner"}}}})
	resolver := NewWorkloadOwnerResolver(client, time.Minute)
	owner := metav1.OwnerReference{APIVersion: "apps/v1", Kind: "ReplicaSet", Name: "replica", UID: "old-uid"}
	_, _, err := resolver.ResolveReference(t.Context(), "team-a", owner, time.Now())
	if !errors.Is(err, ErrOwnerReplaced) {
		t.Fatalf("error=%v", err)
	}
	owner.UID = "new-uid"
	kind, name, err := resolver.ResolveReference(t.Context(), "team-a", owner, time.Now())
	if err != nil || kind != "Deployment" || name != "new-owner" {
		t.Fatalf("%s %s %v", kind, name, err)
	}
	owner.UID = "old-uid"
	if _, _, err := resolver.ResolveReference(t.Context(), "team-a", owner, time.Now()); !errors.Is(err, ErrOwnerReplaced) {
		t.Fatal("cache crossed controller identities")
	}
}

func TestOwnerReferenceDoesNotProbeADifferentAPIGroup(t *testing.T) {
	client := fake.NewSimpleClientset()
	resolver := NewWorkloadOwnerResolver(client, time.Minute)
	owner := metav1.OwnerReference{APIVersion: "example.invalid/v1", Kind: "ReplicaSet", Name: "replica", UID: "uid"}
	if _, _, err := resolver.ResolveReference(t.Context(), "team-a", owner, time.Now()); !errors.Is(err, ErrOwnerAPI) {
		t.Fatalf("error=%v", err)
	}
	if len(client.Actions()) != 0 {
		t.Fatal("queried an unrelated API group")
	}
}
