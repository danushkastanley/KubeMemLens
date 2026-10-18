package kube

import (
	"errors"
	"strings"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/memoryhistory"
	"github.com/danushkastanley/kube-memlens/internal/replicabaseline"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestReplicaRevisionUsesVerifiedControllerRevision(t *testing.T) {
	for _, kind := range []string{"StatefulSet", "DaemonSet"} {
		for _, change := range []string{"valid", "wrong owner", "deleted", "denied", "unreported"} {
			t.Run(kind+"/"+change, func(t *testing.T) {
				f, r := newReplicaFixture(t)
				resource, _ := volumeWorkloadResource(kind)
				root := f.deployment
				root.Kind = kind
				root.UID = "root-uid"
				controller := true
				owner := metav1.OwnerReference{APIVersion: "apps/v1", Kind: kind, Name: root.Name, UID: root.UID, Controller: &controller}
				revision := workloadObject{TypeMeta: metav1.TypeMeta{Kind: "ControllerRevision", APIVersion: "apps/v1"}, ObjectMeta: metav1.ObjectMeta{Namespace: "tenant-a", Name: "app-abc123", UID: "revision-uid", CreationTimestamp: root.CreationTimestamp, OwnerReferences: []metav1.OwnerReference{owner}}}
				f.pod.OwnerReferences = []metav1.OwnerReference{owner}
				label := revision.Name
				if kind == "DaemonSet" {
					label = "abc123"
				}
				f.pod.Labels["controller-revision-hash"] = label
				switch change {
				case "wrong owner":
					revision.OwnerReferences[0].UID = "other-root"
				case "deleted":
					revision.DeletionTimestamp = &revision.CreationTimestamp
				case "denied":
					f.denied["apps/controllerrevisions/"] = true
				case "unreported":
					delete(f.pod.Labels, "controller-revision-hash")
				}
				path := "/apis/apps/v1/namespaces/tenant-a/controllerrevisions/" + revision.Name
				f.extra = map[string]any{resource.path("tenant-a") + "/app": root, path: revision}
				selected, err := r.Resolve(t.Context(), f.selection.Request)
				switch change {
				case "wrong owner", "deleted":
					if !errors.Is(err, memoryhistory.ErrChanged) {
						t.Fatal("unverified revision accepted", err)
					}
				case "denied":
					if !errors.Is(err, memoryhistory.ErrDenied) {
						t.Fatal("revision permission ignored", err)
					}
					for _, read := range f.reads {
						if strings.Contains(read, "/controllerrevisions/") {
							t.Fatal("denied revision read")
						}
					}
				default:
					if err != nil {
						t.Fatal(err)
					}
					want := "revision-uid"
					if change == "unreported" {
						want = ""
					}
					if selected.Members[0].Revision != want {
						t.Fatal("incorrect revision", selected.Members[0].Revision)
					}
				}
			})
		}
	}
}

func TestReplicaResolverExcludesForeignSelectorMatches(t *testing.T) {
	f, r := newReplicaFixture(t)
	// A matching selector is not evidence that the Pod belongs to this root.
	f.pod.OwnerReferences = nil
	request := memoryhistory.Request{Scope: memoryhistory.Workload, Namespace: "tenant-a", Name: "app", WorkloadKind: "Deployment"}
	selected, err := r.Resolve(t.Context(), request)
	if err != nil || len(selected.Members) != 0 {
		t.Fatalf("foreign member retained: %+v %v", selected, err)
	}
}

func TestReplicaResolverConservativeStandaloneRevision(t *testing.T) {
	f, r := newReplicaFixture(t)
	f.rs.OwnerReferences = nil
	selected, err := r.Resolve(t.Context(), f.selection.Request)
	if err != nil {
		t.Fatal(err)
	}
	if selected.Members[0].Revision != "" || selected.Members[0].Changes != replicabaseline.ChangeUnknown {
		t.Fatal("standalone controller treated as verified unchanged template")
	}
}
