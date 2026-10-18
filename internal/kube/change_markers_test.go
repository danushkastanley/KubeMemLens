package kube

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/changemarkers"
	"github.com/danushkastanley/kube-memlens/internal/memoryhistory"
)

func TestMarkerProviderPreservesSourceAndExcludesPrivateEventData(t *testing.T) {
	f := newMarkerFixture(t)
	f.edit(func() {
		f.events = append(f.events, f.event("old-instance", "old-pod-uid", "ResizeCompleted"), f.event("unknown", "pod-uid", "PRIVATE REASON"))
	})
	r, err := f.provider.Query(t.Context(), f.selection, f.query)
	if err != nil {
		t.Fatal(err)
	}
	if r.Events != changemarkers.Partial || len(r.Markers) != 5 {
		t.Fatalf("expected creation, restart, condition, event and revision: %+v", r)
	}
	for _, m := range r.Markers {
		if m.Subject.UID != "pod-uid" || len(m.Owners) != 2 || m.Owners[0].UID != "rs-uid" || m.Owners[1].UID != "deployment-uid" {
			t.Fatal("owner chain lost", m)
		}
		if m.Kind == changemarkers.Revision && (!m.At.Equal(f.now) || m.SourceUID != "rs-uid") {
			t.Fatal("revision source was retroactively timed", m)
		}
		if m.Clock == changemarkers.KubernetesEvent && (m.SourceUID != "resize-event" || m.ResizeState != "completed" || !m.At.Equal(f.now.Add(-time.Minute))) {
			t.Fatal("event source changed", m)
		}
	}
	body, _ := json.Marshal(r)
	if strings.Contains(string(body), "PRIVATE") || strings.Contains(string(body), "old-pod-uid") {
		t.Fatal("unselected or free-form evidence disclosed")
	}
	if err := f.provider.Revalidate(t.Context(), r); err != nil {
		t.Fatal(err)
	}
}

func TestMarkerProviderDeniedSelectionDoesNotAcquire(t *testing.T) {
	f := newMarkerFixture(t)
	f.denied[api.MemoryAPIGroup+"/pods/trends-context"] = true
	if _, err := f.provider.Query(t.Context(), f.selection, f.query); !errors.Is(err, memoryhistory.ErrDenied) {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.reads) != 0 {
		t.Fatal("denied selection acquired objects")
	}
}

func TestMarkerProviderRechecksEventsAndOwnerIdentity(t *testing.T) {
	for _, change := range []string{"event permission", "owner replaced", "owner changed"} {
		t.Run(change, func(t *testing.T) {
			f := newMarkerFixture(t)
			r, err := f.provider.Query(t.Context(), f.selection, f.query)
			if err != nil {
				t.Fatal(err)
			}
			f.edit(func() {
				switch change {
				case "event permission":
					f.denied["/events/"] = true
				case "owner replaced":
					f.rs.UID = "other-rs-uid"
				case "owner changed":
					f.rs.OwnerReferences = nil
				}
			})
			if err := f.provider.Revalidate(t.Context(), r); err == nil {
				t.Fatal("revoked or rebound evidence disclosed")
			}
		})
	}
}

func TestMarkerEventsHaveExplicitMissingDeniedAndUnavailableStates(t *testing.T) {
	for _, state := range []changemarkers.Coverage{changemarkers.Missing, changemarkers.Denied, changemarkers.Unavailable} {
		t.Run(string(state), func(t *testing.T) {
			f := newMarkerFixture(t)
			f.edit(func() {
				switch state {
				case changemarkers.Missing:
					f.events = nil
				case changemarkers.Denied:
					f.denied["/events/"] = true
				case changemarkers.Unavailable:
					f.eventStatus = 503
				}
			})
			r, err := f.provider.Query(t.Context(), f.selection, f.query)
			if err != nil || r.Events != state || len(r.Markers) != 4 {
				t.Fatalf("state=%s report=%+v error=%v", state, r, err)
			}
			if state == changemarkers.Denied {
				f.mu.Lock()
				defer f.mu.Unlock()
				for _, path := range f.reads {
					if strings.HasSuffix(path, "/events") {
						t.Fatal("denied event source queried")
					}
				}
			}
		})
	}
}

func TestMarkerProviderRetainsBoundedEventPageWithPartialCoverage(t *testing.T) {
	f := newMarkerFixture(t)
	f.edit(func() {
		f.events = nil
		for i := 0; i < 257; i++ {
			f.events = append(f.events, f.event(fmt.Sprintf("event-%03d", i), "pod-uid", "Started"))
		}
		f.continued = true
	})
	r, err := f.provider.Query(t.Context(), f.selection, f.query)
	if err != nil || !r.Truncated || len(r.Markers) != changemarkers.MaxMarkers || r.Events != changemarkers.Partial {
		t.Fatalf("bounded page: count=%d truncated=%v error=%v", len(r.Markers), r.Truncated, err)
	}
}

func TestMarkerProviderRejectsChangedContainerLifetimeAndOwnerCycles(t *testing.T) {
	for _, change := range []string{"container", "cycle", "same-name-pod"} {
		t.Run(change, func(t *testing.T) {
			f := newMarkerFixture(t)
			f.edit(func() {
				switch change {
				case "container":
					f.pod.Status.ContainerStatuses[0].ContainerID = "containerd://replacement"
				case "cycle":
					f.rs.OwnerReferences[0].Kind = "ReplicaSet"
					f.rs.OwnerReferences[0].Name = "app-rs"
					f.rs.OwnerReferences[0].UID = f.rs.UID
				case "same-name-pod":
					f.pod.UID = "replacement"
				}
			})
			if _, err := f.provider.Query(t.Context(), f.selection, f.query); err == nil {
				t.Fatal("changed lifetime accepted")
			}
		})
	}
}

func TestMarkerProviderSupportsWorkloadSelection(t *testing.T) {
	f := newMarkerFixture(t)
	f.selection.Request = memoryhistory.Request{Scope: memoryhistory.Workload, Namespace: "tenant-a", Name: "app", WorkloadKind: "Deployment"}
	f.selection.UID = "deployment-uid"
	r, err := f.provider.Query(t.Context(), f.selection, f.query)
	if err != nil {
		t.Fatal(err)
	}
	if r.UID != "deployment-uid" || len(r.Markers) != 7 {
		t.Fatal("workload ownership/revision context lost", r)
	}
	if err := f.provider.Revalidate(t.Context(), r); err != nil {
		t.Fatal(err)
	}
}

func TestContextResolverRequiresBothNamedPermissionsBeforeAnyRead(t *testing.T) {
	for _, subresource := range []string{"trends-context", "trends"} {
		t.Run(subresource, func(t *testing.T) {
			f := newMarkerFixture(t)
			f.denied[api.MemoryAPIGroup+"/pods/"+subresource] = true
			resolver, err := NewMemoryHistoryContextResolver(f.config, f.provider.authorize, func(string, time.Time) (string, bool) { return "node-uid", true })
			if err != nil {
				t.Fatal(err)
			}
			if _, err := resolver.Resolve(t.Context(), f.selection.Request); !errors.Is(err, memoryhistory.ErrDenied) {
				t.Fatal(err)
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			if len(f.reads) != 0 {
				t.Fatal("denied named context acquired an object")
			}
		})
	}
}
