package kube

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/changemarkers"
	"github.com/danushkastanley/kube-memlens/internal/memoryhistory"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestMaximumCronJobContextResolutionSequence(t *testing.T) {
	f := maximumCronJobMarkerFixture(t)
	f.provider.now = time.Now
	resolver, err := NewMemoryHistoryContextResolver(f.config, f.provider.authorize, func(string, time.Time) (string, bool) {
		return "node-uid", true
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 12*time.Second)
	defer cancel()
	start := time.Now()
	selected, err := resolver.Resolve(ctx, f.selection.Request)
	if err != nil {
		t.Fatal("initial selection", err)
	}
	changes, err := f.provider.Query(ctx, selected, f.query)
	if err != nil {
		t.Fatal("marker acquisition", err)
	}
	current, err := resolver.Resolve(ctx, f.selection.Request)
	if err != nil {
		t.Fatal("selection revalidation", err)
	}
	if selected.UID != current.UID || selected.Revision != current.Revision || !slices.Equal(selected.Targets, current.Targets) {
		t.Fatal("stable cohort changed between phases")
	}
	if err := f.provider.Revalidate(ctx, changes); err != nil {
		t.Fatal("marker revalidation", err)
	}
	if len(current.Targets) != 16 {
		t.Fatalf("targets=%d, want 16", len(current.Targets))
	}
	t.Logf("complete source sequence=%s; external history provider time excluded", time.Since(start))
}

func TestContextSelectionRefreshesIdentityAndPermissionsBetweenPhases(t *testing.T) {
	for _, change := range []string{"Pod permission", "owner permission", "replacement", "restart", "generation", "deletion", "unsupported owner"} {
		t.Run(change, func(t *testing.T) {
			f := newMarkerFixtureAt(t, time.Now().UTC().Truncate(time.Second))
			request := memoryhistory.Request{Scope: memoryhistory.Workload, Namespace: "tenant-a", Name: "app", WorkloadKind: "Deployment"}
			resolver, err := NewMemoryHistoryContextResolver(f.config, f.provider.authorize, func(string, time.Time) (string, bool) { return "node-uid", true })
			if err != nil {
				t.Fatal(err)
			}
			before, err := resolver.Resolve(t.Context(), request)
			if err != nil {
				t.Fatal(err)
			}
			f.edit(func() {
				switch change {
				case "Pod permission":
					f.denied["/pods/"] = true
				case "owner permission":
					f.denied["apps/replicasets/"] = true
				case "replacement":
					f.pod.UID = "replacement"
				case "restart":
					f.pod.Status.ContainerStatuses[0].ContainerID = "containerd://new"
				case "generation":
					f.deployment.Generation++
				case "deletion":
					at := metav1.NewTime(f.now)
					f.pod.DeletionTimestamp = &at
				case "unsupported owner":
					f.pod.OwnerReferences[0].Kind = "CustomController"
				}
			})
			after, err := resolver.Resolve(t.Context(), request)
			switch change {
			case "Pod permission", "owner permission":
				if !errors.Is(err, memoryhistory.ErrDenied) {
					t.Fatal("revoked permission reused", err)
				}
			case "deletion":
				if !errors.Is(err, memoryhistory.ErrChanged) {
					t.Fatal("deleted Pod retained", err)
				}
			case "unsupported owner":
				if !errors.Is(err, changemarkers.ErrUnsupported) {
					t.Fatal("unsupported owner error lost", err)
				}
			default:
				if err != nil || (before.Revision == after.Revision && slices.Equal(before.Targets, after.Targets)) {
					t.Fatal("changed selection not visible to disclosure guard", err)
				}
			}
		})
	}
}
