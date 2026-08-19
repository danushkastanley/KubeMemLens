package kube

import (
	"context"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestWorkloadOwnerResolverFollowsReplicaSetAndJob(t *testing.T) {
	controller := true
	client := fake.NewSimpleClientset(
		&appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{
			Namespace: "default", Name: "api-abc", OwnerReferences: []metav1.OwnerReference{{Kind: "Deployment", Name: "api", Controller: &controller}},
		}},
		&batchv1.Job{ObjectMeta: metav1.ObjectMeta{
			Namespace: "default", Name: "backup-123", OwnerReferences: []metav1.OwnerReference{{Kind: "CronJob", Name: "backup", Controller: &controller}},
		}},
	)
	resolver := NewWorkloadOwnerResolver(client, time.Minute)
	now := time.Unix(1_700_000_000, 0).UTC()

	for _, test := range []struct {
		kind, name, wantKind, wantName string
	}{
		{kind: "ReplicaSet", name: "api-abc", wantKind: "Deployment", wantName: "api"},
		{kind: "Job", name: "backup-123", wantKind: "CronJob", wantName: "backup"},
		{kind: "StatefulSet", name: "database", wantKind: "StatefulSet", wantName: "database"},
	} {
		kind, name, err := resolver.Resolve(context.Background(), "default", test.kind, test.name, now)
		if err != nil {
			t.Fatalf("Resolve(%s) returned error: %v", test.kind, err)
		}
		if kind != test.wantKind || name != test.wantName {
			t.Fatalf("Resolve(%s) = %s/%s, want %s/%s", test.kind, kind, name, test.wantKind, test.wantName)
		}
	}
}

func TestWorkloadOwnerResolverCachesAndDeduplicatesLookups(t *testing.T) {
	controller := true
	client := fake.NewSimpleClientset(&appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{
		Namespace: "default", Name: "api-abc", OwnerReferences: []metav1.OwnerReference{{Kind: "Deployment", Name: "api", Controller: &controller}},
	}})
	resolver := NewWorkloadOwnerResolver(client, time.Minute)
	idx := EmptyPodIndex()
	for _, id := range []string{"id-a", "id-b"} {
		idx.ByContainerID[id] = PodRef{Namespace: "default", PodUID: "uid-a", ContainerID: id, Context: containerOwner("ReplicaSet", "api-abc")}
	}

	resolved, errors := resolver.ResolveIndex(context.Background(), idx, time.Now().UTC())
	if errors != 0 {
		t.Fatalf("resolution errors = %d, want 0", errors)
	}
	if actions := len(client.Actions()); actions != 1 {
		t.Fatalf("API actions = %d, want one deduplicated GET", actions)
	}
	for _, ref := range resolved.ByContainerID {
		if ref.Context.WorkloadKind != "Deployment" || ref.Context.WorkloadName != "api" {
			t.Fatalf("unexpected resolved context: %#v", ref.Context)
		}
	}
	_, _ = resolver.ResolveIndex(context.Background(), idx, time.Now().UTC())
	if actions := len(client.Actions()); actions != 1 {
		t.Fatalf("API actions after cache hit = %d, want 1", actions)
	}
}

func containerOwner(kind, name string) api.ContainerContext {
	return api.ContainerContext{OwnerKind: kind, OwnerName: name}
}
