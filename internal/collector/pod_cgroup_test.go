package collector

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/model"
)

func podCgroupContainers(now time.Time, oom uint64) []api.ContainerSnapshot {
	cgroup := model.PodCgroupMemory{CurrentBytes: 300, CurrentKnown: true, MaxBytes: 256 << 20, MaxKnown: true, LocalEventsKnown: true, LocalOOMEvents: oom}
	app, sidecar := container("tenant-a", "app", "app", "id-app", now, 100), container("tenant-a", "app", "sidecar", "id-sidecar", now, 50)
	app.PodCgroup, sidecar.PodCgroup = cgroup, cgroup
	app.Memory.OOMGroupKill = model.OOMGroupKill{Known: true, Events: oom}
	return []api.ContainerSnapshot{app, sidecar}
}

func TestSnapshotPodCgroupSchemaValidation(t *testing.T) {
	now := time.Unix(1000, 0).UTC()
	opts := defaultHandlerOptions(HandlerOptions{})
	valid := api.AgentSnapshot{SchemaVersion: api.PodCgroupSnapshotSchemaVersion, NodeName: "node-a", CapturedAt: now, Containers: podCgroupContainers(now, 2)}
	if err := ValidateSnapshot(valid, now, opts); err != nil {
		t.Fatal(err)
	}
	if err := ValidateSnapshot(api.AgentSnapshotForSchema(valid, api.TopologySnapshotSchemaVersion), now, opts); err != nil {
		t.Fatalf("projected schema 7 snapshot rejected: %v", err)
	}
	for name, change := range map[string]func(*api.AgentSnapshot){
		"Pod cgroup below schema 8": func(s *api.AgentSnapshot) {
			s.SchemaVersion = api.TopologySnapshotSchemaVersion
			s.Containers[0].Memory.OOMGroupKill = model.OOMGroupKill{}
		},
		"zswap below schema 8": func(s *api.AgentSnapshot) {
			s.SchemaVersion = api.TopologySnapshotSchemaVersion
			s.Containers = s.Containers[1:]
			s.Containers[0].PodCgroup = model.PodCgroupMemory{}
			s.Containers[0].Memory.Zswap = model.Zswap{Known: true, PoolBytes: 1}
		},
		"oom_group_kill below schema 8": func(s *api.AgentSnapshot) {
			s.SchemaVersion = api.IOPressureSnapshotSchemaVersion
			s.Containers = s.Containers[:1]
			s.Containers[0].PodCgroup = model.PodCgroupMemory{}
		},
		"unknown value with bytes": func(s *api.AgentSnapshot) {
			for index := range s.Containers {
				s.Containers[index].PodCgroup.CurrentKnown = false
			}
		},
		"unknown events with count": func(s *api.AgentSnapshot) { s.Containers[0].Memory.OOMGroupKill.Known = false },
		"containers disagree":       func(s *api.AgentSnapshot) { s.Containers[1].PodCgroup.MaxBytes = 128 << 20 },
		"no Pod to belong to":       func(s *api.AgentSnapshot) { s.Containers[1].PodUID = "" },
	} {
		t.Run(name, func(t *testing.T) {
			snapshot := valid
			snapshot.Containers = podCgroupContainers(now, 2)
			change(&snapshot)
			if err := ValidateSnapshot(snapshot, now, opts); err == nil {
				t.Fatal("invalid schema 8 evidence accepted")
			}
		})
	}
}

// The collector owns every Pod cgroup delta: it is computed per Pod from the
// previous stored snapshot, is the same on every container of the Pod, is
// zero on a first sample whatever the agent sent, and is zero, not a wrapped
// unsigned value, when the counter goes down.
func TestStoreDerivesPodCgroupDeltasPerPod(t *testing.T) {
	now := time.Unix(1000, 0).UTC()
	store := NewStore()
	stored := func(at time.Time, containers []api.ContainerSnapshot) []api.ContainerSnapshot {
		t.Helper()
		if _, err := store.ReplaceNodeSnapshot(api.AgentSnapshot{NodeName: "node-a", CapturedAt: at, Containers: containers}); err != nil {
			t.Fatal(err)
		}
		items := store.ListContainers(at, time.Minute)
		if len(items) != 2 || items[0].PodCgroup != items[1].PodCgroup {
			t.Fatalf("containers of one Pod disagree after deltas: %#v", items)
		}
		return items
	}

	first := podCgroupContainers(now, 5)
	for index := range first {
		first[index].PodCgroup.LocalEventDeltasKnown, first[index].PodCgroup.LocalOOMEventsDelta = true, 5
	}
	items := stored(now, first)
	if got := items[0].PodCgroup; !got.LocalEventDeltasKnown || got.LocalOOMEventsDelta != 0 {
		t.Fatalf("first sample = %#v, want known deltas of zero", got)
	}
	if got := items[1].Memory.OOMGroupKill; got != (model.OOMGroupKill{}) {
		t.Fatalf("a container without oom_group_kill gained %#v", got)
	}

	items = stored(now.Add(time.Second), podCgroupContainers(now.Add(time.Second), 7))
	if got := items[0].PodCgroup.LocalOOMEventsDelta; got != 2 {
		t.Fatalf("delta = %d, want 2", got)
	}
	if got := findContainer(items, "id-app").Memory.OOMGroupKill; !got.DeltasKnown || got.EventsDelta != 2 {
		t.Fatalf("oom_group_kill = %#v, want a delta of 2", got)
	}

	items = stored(now.Add(2*time.Second), podCgroupContainers(now.Add(2*time.Second), 1))
	if got := items[0].PodCgroup; !got.LocalEventDeltasKnown || got.LocalOOMEventsDelta != 0 {
		t.Fatalf("after a counter reset = %#v, want a known zero delta", got)
	}

	replaced := podCgroupContainers(now.Add(3*time.Second), 9)
	for index := range replaced {
		replaced[index].PodUID = "tenant-a-app-uid-2"
	}
	if items = stored(now.Add(3*time.Second), replaced); items[0].PodCgroup.LocalOOMEventsDelta != 0 {
		t.Fatalf("a new Pod UID inherited the old Pod's counters: %#v", items[0].PodCgroup)
	}
}

func TestNamespaceReadIsProjectedForOlderReaders(t *testing.T) {
	now := time.Now().UTC()
	store := NewStore()
	if _, err := store.ReplaceNodeSnapshot(api.AgentSnapshot{NodeName: "node-a", CapturedAt: now, Containers: podCgroupContainers(now, 2)}); err != nil {
		t.Fatal(err)
	}
	handler := NewReadHandlerWithOptions(store, DefaultHandlerOptions(time.Minute))
	for header, wantEvidence := range map[string]bool{"": false, "7": false, "8": true} {
		request := httptest.NewRequest(http.MethodGet, "/api/v1/namespaces", nil)
		if header != "" {
			request.Header.Set(api.SnapshotSchemaHeader, header)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("schema %q status = %d", header, response.Code)
		}
		if got := strings.Contains(response.Body.String(), `"oomGroupKill"`); got != wantEvidence {
			t.Fatalf("schema %q namespaces carry oomGroupKill = %t: %s", header, got, response.Body)
		}
	}
}

func findContainer(items []api.ContainerSnapshot, id string) api.ContainerSnapshot {
	for _, item := range items {
		if item.ContainerID == id {
			return item
		}
	}
	return api.ContainerSnapshot{}
}
