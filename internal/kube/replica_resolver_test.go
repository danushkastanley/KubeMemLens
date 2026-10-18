package kube

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/changemarkers"
	"github.com/danushkastanley/kube-memlens/internal/memoryhistory"
	"github.com/danushkastanley/kube-memlens/internal/replicabaseline"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func newReplicaFixture(t *testing.T) (*markerFixture, *replicaResolver) {
	t.Helper()
	f := newMarkerFixture(t)
	f.edit(func() {
		f.pod.CreationTimestamp = metav1.NewTime(f.now.Add(-time.Hour))
		f.pod.Spec.Containers = []corev1.Container{{Name: "app", Image: "private/image:mutable", Env: []corev1.EnvVar{{Name: "PRIVATE", Value: "PRIVATE VALUE"}}}}
		f.pod.Status.Phase = corev1.PodRunning
		f.pod.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}
		f.pod.Status.ContainerStatuses[0].ImageID = "containerd://sha256:" + strings.Repeat("a", 64)
		f.pod.Status.ContainerStatuses[0].State.Running.StartedAt = metav1.NewTime(f.now.Add(-time.Hour))
		f.pod.Status.ContainerStatuses[0].RestartCount = 0
		f.events = nil
	})
	value, err := NewReplicaResolver(f.config, f.provider.authorize, func(string, time.Time) (string, bool) { return "node-uid", true })
	if err != nil {
		t.Fatal(err)
	}
	resolver := value.(*replicaResolver)
	resolver.now = func() time.Time { return f.now }
	return f, resolver
}

func TestReplicaResolverPreservesVerifiedIdentityAndPrivateMetadata(t *testing.T) {
	f, r := newReplicaFixture(t)
	selected, err := r.Resolve(t.Context(), f.selection.Request)
	if err != nil {
		t.Fatal(err)
	}
	if selected.Workload.UID != "deployment-uid" || selected.Requested.UID != "pod-uid" || len(selected.Members) != 1 {
		t.Fatalf("selection: %+v", selected)
	}
	m := selected.Members[0]
	if len(m.Containers) != 1 || m.Containers[0].ID != "current" {
		t.Fatal("runtime ID was not normalised to the agent identity")
	}
	if m.Revision != "rs-uid" || m.Shape == "" || m.NodeUID != "node-uid" || m.Lifecycle != replicabaseline.Ready || m.Changes != replicabaseline.ChangeUnknown || len(m.Owners) != 2 {
		t.Fatalf("member: %+v", m)
	}
	encoded, err := json.Marshal(selected)
	if err != nil || strings.Contains(string(encoded), "PRIVATE") || strings.Contains(string(encoded), "private/image") {
		t.Fatal("private Pod configuration retained", err)
	}
	if selected.EventState != changemarkers.Missing {
		t.Fatal("missing history misrepresented", selected.EventState)
	}
}

func TestReplicaResolverDeniesBeforeAcquisition(t *testing.T) {
	f, r := newReplicaFixture(t)
	f.denied[api.MemoryAPIGroup+"/pods/replicas"] = true
	if _, err := r.Resolve(t.Context(), f.selection.Request); !errors.Is(err, memoryhistory.ErrDenied) {
		t.Fatal(err)
	}
	if len(f.reads) != 0 {
		t.Fatal("denied request acquired objects", f.reads)
	}
}

func TestReplicaResolverRechecksNamedMemberAndRevisionPermissions(t *testing.T) {
	for _, group := range []string{"", api.MemoryAPIGroup} {
		t.Run(group, func(t *testing.T) {
			_, r := newReplicaFixture(t)
			request := memoryhistory.Request{Scope: memoryhistory.Workload, Namespace: "tenant-a", Name: "app", WorkloadKind: "Deployment"}
			if _, err := r.Resolve(t.Context(), request); err != nil {
				t.Fatal(err)
			}
			original := r.authorize
			r.authorize = func(ctx context.Context, a ObjectAccess) error {
				if a.Group == group && a.Resource == "pods" && a.Name == "app" {
					return memoryhistory.ErrDenied
				}
				return original(ctx, a)
			}
			if selected, err := r.Resolve(t.Context(), request); !errors.Is(err, memoryhistory.ErrDenied) || len(selected.Members) != 0 {
				t.Fatalf("revoked member disclosed: %+v %v", selected, err)
			}
		})
	}
}

func TestReplicaResolverRejectsRootReplacementDuringAcquisition(t *testing.T) {
	f, r := newReplicaFixture(t)
	f.beforeRead = func(req *http.Request) {
		if strings.HasSuffix(req.URL.Path, "/events") {
			f.edit(func() { f.deployment.UID = "replacement" })
		}
	}
	if _, err := r.Resolve(t.Context(), f.selection.Request); !errors.Is(err, memoryhistory.ErrChanged) {
		t.Fatal("root replacement accepted", err)
	}
}

func TestReplicaResolverReportsRecentAndUnknownChanges(t *testing.T) {
	for _, state := range []string{"recent event", "denied events", "future event", "resize"} {
		t.Run(state, func(t *testing.T) {
			f, r := newReplicaFixture(t)
			switch state {
			case "recent event":
				f.events = []map[string]any{f.event("resize", "pod-uid", "ResizeCompleted")}
			case "future event":
				event := f.event("resize", "pod-uid", "ResizeCompleted")
				event["eventTime"] = metav1.NewMicroTime(f.now.Add(time.Minute))
				f.events = []map[string]any{event}
			case "denied events":
				f.denied["/events/"] = true
			case "resize":
				f.pod.Status.Conditions = append(f.pod.Status.Conditions, corev1.PodCondition{Type: corev1.PodResizeInProgress, Status: corev1.ConditionTrue})
			}
			selected, err := r.Resolve(t.Context(), f.selection.Request)
			if state == "future event" {
				if !errors.Is(err, replicabaseline.ErrInvalid) {
					t.Fatal(err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			want := replicabaseline.RecentChange
			if state == "denied events" {
				want = replicabaseline.ChangeUnknown
				if selected.EventState != changemarkers.Denied {
					t.Fatal(selected.EventState)
				}
			}
			if selected.Members[0].Changes != want {
				t.Fatal("change coverage", selected.Members[0].Changes)
			}
		})
	}
}
