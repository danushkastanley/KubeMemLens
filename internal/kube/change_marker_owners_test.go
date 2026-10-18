package kube

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/changemarkers"
	"github.com/danushkastanley/kube-memlens/internal/memoryhistory"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestMarkerWorkloadFamiliesPreserveVerifiedOwnerChains(t *testing.T) {
	for _, kind := range []string{"Deployment", "ReplicaSet", "StatefulSet", "DaemonSet", "ReplicationController", "Job", "CronJob"} {
		t.Run(kind, func(t *testing.T) {
			f := newMarkerFixtureAt(t, time.Now().UTC().Truncate(time.Second))
			resource, _ := volumeWorkloadResource(kind)
			root := f.deployment
			root.TypeMeta = metav1.TypeMeta{Kind: kind, APIVersion: resource.version}
			root.ObjectMeta = *f.deployment.ObjectMeta.DeepCopy()
			root.Name = "root"
			root.UID = "root-uid"
			root.Annotations = nil
			if kind == "ReplicationController" {
				root.Spec.Selector = json.RawMessage(`{"app":"fixture"}`)
			}
			controller := true
			owner := metav1.OwnerReference{APIVersion: resource.version, Kind: kind, Name: root.Name, UID: root.UID, Controller: &controller}
			f.selection.Request = memoryhistory.Request{Scope: memoryhistory.Workload, Namespace: "tenant-a", Name: root.Name, WorkloadKind: kind}
			f.selection.UID = string(root.UID)
			f.extra = map[string]any{resource.path("tenant-a") + "/root": root}
			expectedDepth := 1
			if kind == "Deployment" || kind == "CronJob" {
				bridgeKind := "ReplicaSet"
				if kind == "CronJob" {
					bridgeKind = "Job"
				}
				bridgeResource, _ := volumeWorkloadResource(bridgeKind)
				bridge := f.rs
				bridge.ObjectMeta = *f.rs.ObjectMeta.DeepCopy()
				bridge.TypeMeta = metav1.TypeMeta{Kind: bridgeKind, APIVersion: bridgeResource.version}
				bridge.Name = "bridge"
				bridge.UID = "bridge-uid"
				bridge.OwnerReferences = []metav1.OwnerReference{owner}
				bridge.Annotations = nil
				f.extra[bridgeResource.path("tenant-a")+"/bridge"] = bridge
				if kind == "CronJob" {
					f.extra[bridgeResource.path("tenant-a")] = workloadList{TypeMeta: metav1.TypeMeta{Kind: "JobList", APIVersion: "batch/v1"}, Items: []workloadObject{bridge}}
				}
				owner = metav1.OwnerReference{APIVersion: bridgeResource.version, Kind: bridgeKind, Name: bridge.Name, UID: bridge.UID, Controller: &controller}
				expectedDepth = 2
			}
			f.pod.OwnerReferences = []metav1.OwnerReference{owner}
			result, err := f.provider.Query(t.Context(), f.selection, f.query)
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Markers) == 0 {
				t.Fatal("workload changes missing")
			}
			for _, m := range result.Markers {
				if m.Subject.Kind == "Pod" && (len(m.Owners) != expectedDepth || m.Owners[len(m.Owners)-1].UID != string(root.UID)) {
					t.Fatal("owner chain changed", m)
				}
			}
			if err := f.provider.Revalidate(t.Context(), result); err != nil {
				t.Fatal(err)
			}
			resolver, err := NewMemoryHistoryContextResolver(f.config, f.provider.authorize, func(string, time.Time) (string, bool) { return "node-uid", true })
			if err != nil {
				t.Fatal(err)
			}
			selected, err := resolver.Resolve(t.Context(), f.selection.Request)
			if err != nil || selected.UID != string(root.UID) || len(selected.Targets) != 1 {
				t.Fatalf("context selection: %+v, %v", selected, err)
			}
			if kind == "CronJob" {
				prior := f.provider.authorize
				f.provider.authorize = func(ctx context.Context, a ObjectAccess) error {
					if a.Group == "batch" && a.Resource == "jobs" && a.Name == "bridge" {
						return memoryhistory.ErrDenied
					}
					return prior(ctx, a)
				}
				if err := f.provider.Revalidate(t.Context(), result); !errors.Is(err, memoryhistory.ErrDenied) {
					t.Fatal("listed Job bypassed revoked named permission", err)
				}
			}
		})
	}
}

func TestMarkerWorkloadSnapshotRetainsNamedPermissionsAndFreshLifetimes(t *testing.T) {
	for _, change := range []string{"named Pod revoked", "same-name replacement", "owner reparented", "item type omitted", "wrong item type"} {
		t.Run(change, func(t *testing.T) {
			f := newMarkerFixture(t)
			f.selection.Request = memoryhistory.Request{Scope: memoryhistory.Workload, Namespace: "tenant-a", Name: "app", WorkloadKind: "Deployment"}
			f.selection.UID = "deployment-uid"
			result, err := f.provider.Query(t.Context(), f.selection, f.query)
			if err != nil {
				t.Fatal(err)
			}
			f.edit(func() {
				switch change {
				case "named Pod revoked":
					f.denied["/pods/"] = true
				case "same-name replacement":
					f.pod.UID = types.UID("replacement")
				case "owner reparented":
					f.pod.OwnerReferences = nil
				case "item type omitted":
					f.pod.TypeMeta = metav1.TypeMeta{}
				case "wrong item type":
					f.pod.Kind = "Secret"
				}
			})
			err = f.provider.Revalidate(t.Context(), result)
			if (err == nil) != (change == "item type omitted") {
				t.Fatalf("snapshot validation: %v", err)
			}
		})
	}
}

func TestMarkerSnapshotRejectsIncompletePodPage(t *testing.T) {
	f := newMarkerFixture(t)
	f.selection.Request = memoryhistory.Request{Scope: memoryhistory.Workload, Namespace: "tenant-a", Name: "app", WorkloadKind: "Deployment"}
	f.selection.UID = "deployment-uid"
	f.extra = map[string]any{"/api/v1/namespaces/tenant-a/pods": corev1.PodList{TypeMeta: metav1.TypeMeta{Kind: "PodList", APIVersion: "v1"}, ListMeta: metav1.ListMeta{Continue: "unread-page"}, Items: []corev1.Pod{f.pod}}}
	if _, err := f.provider.Query(t.Context(), f.selection, f.query); err == nil {
		t.Fatal("incomplete member snapshot accepted")
	}
}

func TestMarkerUnsupportedOwnersDoNotAcquireBroaderResources(t *testing.T) {
	for _, kind := range []string{"Node", "CustomWorkload"} {
		t.Run(kind, func(t *testing.T) {
			f := newMarkerFixture(t)
			f.pod.OwnerReferences[0].Kind = kind
			f.pod.OwnerReferences[0].APIVersion = "v1"
			if _, err := f.provider.Query(t.Context(), f.selection, f.query); !errors.Is(err, changemarkers.ErrUnsupported) {
				t.Fatal("unsupported owner was not explicit", err)
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			if len(f.reads) != 1 || f.reads[0] != "/api/v1/namespaces/tenant-a/pods/app" {
				t.Fatal("unsupported owner triggered broader acquisition", f.reads)
			}
		})
	}
}
