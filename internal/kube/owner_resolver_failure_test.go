package kube

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
)

func ownedReplicaSet(name string) *appsv1.ReplicaSet {
	controller := true
	return &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{
		Namespace: "default", Name: name,
		OwnerReferences: []metav1.OwnerReference{{Kind: "Deployment", Name: "deploy-" + name, Controller: &controller}},
	}}
}

func indexOwnedBy(replicaSets ...string) PodIndex {
	idx := EmptyPodIndex()
	for _, name := range replicaSets {
		idx.ByContainerID["id-"+name] = PodRef{Namespace: "default", PodUID: "uid-" + name, ContainerID: "id-" + name, Context: containerOwner("ReplicaSet", name)}
	}
	return idx
}

// failingReplicaSets makes ReplicaSet reads fail with the error held in *failure
// while it is set, and counts every read that reaches the API.
func failingReplicaSets(client *fake.Clientset, failure *error) *int {
	reads := 0
	client.PrependReactor("get", "replicasets", func(clienttesting.Action) (bool, runtime.Object, error) {
		reads++
		return *failure != nil, nil, *failure
	})
	return &reads
}

var forbidden = apierrors.NewForbidden(schema.GroupResource{Group: "apps", Resource: "replicasets"}, "api", errors.New("denied"))

// A scan runs every few seconds. Lookups that the API refuses must not be
// re-issued on every scan.
func TestOwnerResolverDoesNotRetryAFailedLookupOnEveryScan(t *testing.T) {
	var failure error = forbidden
	client := fake.NewSimpleClientset(ownedReplicaSet("api"))
	reads := failingReplicaSets(client, &failure)
	resolver := NewWorkloadOwnerResolver(client, 5*time.Minute)
	idx, now := indexOwnedBy("api"), time.Unix(1_800_000_000, 0).UTC()

	for scan := range 5 {
		if _, failed := resolver.ResolveIndex(context.Background(), idx, now.Add(time.Duration(scan)*5*time.Second)); failed != 1 {
			t.Fatalf("scan %d: resolution errors = %d, want 1", scan, failed)
		}
	}
	if *reads != 1 {
		t.Fatalf("API reads during the failure hold = %d, want 1", *reads)
	}

	failure = nil
	resolved, failed := resolver.ResolveIndex(context.Background(), idx, now.Add(ownerFailureHold))
	if failed != 0 || *reads != 2 || resolved.ByContainerID["id-api"].Context.WorkloadName != "deploy-api" {
		t.Fatalf("after the hold: errors=%d reads=%d context=%+v", failed, *reads, resolved.ByContainerID["id-api"].Context)
	}
}

// A ReplicaSet's owner does not change, so a refresh that fails keeps the
// owner already known and does not mark the workload context as lost.
func TestOwnerResolverKeepsTheKnownOwnerWhenARefreshFails(t *testing.T) {
	var failure error
	client := fake.NewSimpleClientset(ownedReplicaSet("api"))
	reads := failingReplicaSets(client, &failure)
	resolver := NewWorkloadOwnerResolver(client, 5*time.Minute)
	idx, now := indexOwnedBy("api"), time.Unix(1_800_000_000, 0).UTC()
	if _, failed := resolver.ResolveIndex(context.Background(), idx, now); failed != 0 {
		t.Fatalf("initial resolution errors = %d", failed)
	}

	failure = apierrors.NewServiceUnavailable("apiserver restarting")
	afterExpiry := now.Add(7 * time.Minute)
	for scan := range 3 {
		resolved, failed := resolver.ResolveIndex(context.Background(), idx, afterExpiry.Add(time.Duration(scan)*5*time.Second))
		if failed != 0 || resolved.ByContainerID["id-api"].Context.WorkloadName != "deploy-api" {
			t.Fatalf("scan %d: errors=%d context=%+v", scan, failed, resolved.ByContainerID["id-api"].Context)
		}
	}
	if *reads != 2 {
		t.Fatalf("API reads = %d, want the first lookup and one failed refresh", *reads)
	}
}

// A lookup that never reached the API server is not an answer about the owner.
// Holding it would stop a busy node from resolving its owners over the next
// few scans, which is how the time budget is meant to be worked through.
func TestOwnerResolverDoesNotHoldALookupThatNeverReachedTheAPI(t *testing.T) {
	for name, unanswered := range map[string]error{
		"time budget spent": context.DeadlineExceeded,
		"scan cancelled":    context.Canceled,
		// client-go reports this, not a context error, when waiting for its
		// rate limiter would overrun the deadline.
		"rate limiter would overrun the budget": errors.New("client rate limiter Wait returned an error: rate: Wait(n=1) would exceed context deadline"),
		"connection refused":                    errors.New("dial tcp 10.96.0.1:443: connect: connection refused"),
	} {
		t.Run(name, func(t *testing.T) {
			failure := unanswered
			client := fake.NewSimpleClientset(ownedReplicaSet("api"))
			reads := failingReplicaSets(client, &failure)
			resolver := NewWorkloadOwnerResolver(client, 5*time.Minute)
			idx, now := indexOwnedBy("api"), time.Unix(1_800_000_000, 0).UTC()
			if _, failed := resolver.ResolveIndex(context.Background(), idx, now); failed != 1 {
				t.Fatalf("resolution errors = %d, want 1", failed)
			}
			failure = nil
			if _, failed := resolver.ResolveIndex(context.Background(), idx, now.Add(time.Second)); failed != 0 || *reads != 2 {
				t.Fatalf("next scan: errors=%d reads=%d, want an immediate retry", failed, *reads)
			}
		})
	}
}

// Owners first seen in the same scan must not all expire in the same scan,
// which would repeat the start-up burst of lookups every cache lifetime.
func TestOwnerResolverSpreadsCacheExpiry(t *testing.T) {
	names := make([]string, 40)
	objects := make([]runtime.Object, 40)
	for index := range names {
		names[index] = fmt.Sprintf("api-%02d", index)
		objects[index] = ownedReplicaSet(names[index])
	}
	client := fake.NewSimpleClientset(objects...)
	var failure error
	reads := failingReplicaSets(client, &failure)
	ttl := 5 * time.Minute
	resolver := NewWorkloadOwnerResolver(client, ttl)
	idx, now := indexOwnedBy(names...), time.Unix(1_800_000_000, 0).UTC()
	resolver.ResolveIndex(context.Background(), idx, now)

	resolver.ResolveIndex(context.Background(), idx, now.Add(ttl-time.Second))
	if *reads != 40 {
		t.Fatalf("reads before the cache lifetime = %d, want no refresh", *reads-40)
	}
	resolver.ResolveIndex(context.Background(), idx, now.Add(ttl+ttl/10))
	if refreshed := *reads - 40; refreshed < 5 || refreshed > 35 {
		t.Fatalf("refreshed %d of 40 owners half-way through the spread, want a partial refresh", refreshed)
	}
}
