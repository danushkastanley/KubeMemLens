package collector

import (
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/memoryhistory"
	"github.com/danushkastanley/kube-memlens/internal/model"
)

func localHistorySelection(now time.Time) memoryhistory.Selection {
	created := now.Add(-time.Hour)
	return memoryhistory.Selection{Request: memoryhistory.Request{Scope: memoryhistory.Pod, Namespace: "tenant-a", Name: "app"}, UID: "pod-uid", ResolvedAt: now,
		Targets: []memoryhistory.Target{{Namespace: "tenant-a", Pod: "app", PodUID: "pod-uid", Container: "app", ContainerID: "containerd://current", Node: "node-a", NodeUID: "node-uid", StartedAt: created, PodCreatedAt: created}}}
}

func TestLocalMemoryHistoryPreservesIdentityGapsAndZero(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	s := NewStore()
	selection := localHistorySelection(now)
	q := memoryhistory.Query{Source: memoryhistory.Local, Metric: memoryhistory.Charge, Start: now.Add(-2 * time.Minute), End: now, Step: time.Minute}
	for _, sample := range []struct {
		at    time.Time
		uid   string
		bytes uint64
	}{{q.Start, "pod-uid", 0}, {q.Start.Add(time.Minute), "previous-pod", 999}, {q.End, "pod-uid", 100}} {
		_, err := s.ReplaceNodeSnapshot(api.AgentSnapshot{NodeName: "node-a", CapturedAt: sample.at, Containers: []api.ContainerSnapshot{{Namespace: "tenant-a", PodName: "app", PodUID: sample.uid, ContainerName: "app", ContainerID: "containerd://current", NodeName: "node-a", CapturedAt: sample.at, Memory: model.MemoryBreakdown{TotalBytes: sample.bytes}}}})
		if err != nil {
			t.Fatal(err)
		}
	}
	r, err := s.MemoryHistory().Query(t.Context(), selection, q)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Series) != 1 || r.Series[0].Origin != "cgroup-v2" || r.Series[0].Target.Container != "" || r.Query.Metric != memoryhistory.Charge {
		t.Fatal("local Pod charge mislabelled")
	}
	p := r.Series[0].Points
	if p[0].Bytes == nil || *p[0].Bytes != 0 || p[1].Bytes != nil || p[2].Bytes == nil || *p[2].Bytes != 100 {
		t.Fatal("name reuse or gaps corrupted the retained series")
	}
	if r.Completeness != memoryhistory.Partial {
		t.Fatal("collector restart or missing interval hidden")
	}
}

func TestLocalContainerHistoryDoesNotInventComposition(t *testing.T) {
	s := NewStore()
	selection := localHistorySelection(time.Now().UTC().Truncate(time.Second))
	selection.Request.Scope, selection.Request.Container = memoryhistory.Container, "app"
	q := memoryhistory.Query{Source: memoryhistory.Local, Metric: memoryhistory.Charge, Start: selection.ResolvedAt.Add(-time.Minute), End: selection.ResolvedAt, Step: time.Minute}
	r, err := s.MemoryHistory().Query(t.Context(), selection, q)
	if err != nil || r.State != memoryhistory.Unsupported || len(r.Series) != 0 {
		t.Fatalf("unsupported retention: %v %+v", err, r)
	}
}

func TestLocalWorkloadHistoryDeduplicatesPodContributors(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	s := NewStore()
	selection := localHistorySelection(now)
	selection.Request = memoryhistory.Request{Scope: memoryhistory.Workload, Namespace: "tenant-a", Name: "web", WorkloadKind: "Deployment"}
	selection.UID = "deployment-uid"
	other := selection.Targets[0]
	other.Container, other.ContainerID = "sidecar", "containerd://sidecar"
	selection.Targets = append(selection.Targets, other)
	q := memoryhistory.Query{Source: memoryhistory.Local, Metric: memoryhistory.Charge, Start: now.Add(-time.Minute), End: now, Step: time.Minute}
	r, err := s.MemoryHistory().Query(t.Context(), selection, q)
	if err != nil || len(r.Series) != 1 || r.Series[0].Target.Container != "" {
		t.Fatalf("duplicate Pod charge: %v", err)
	}
}
