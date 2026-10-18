package collector

import (
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/memoryhistory"
	"github.com/danushkastanley/kube-memlens/internal/nodecontext"
)

func TestLocalNodeHistoryKeepsZeroGapsAndRecreationSeparate(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	store := NewStore()
	store.EnableNodeContext()
	clock := now.Add(-2 * time.Minute)
	store.now = func() time.Time { return clock }
	for _, at := range []time.Time{clock, now} {
		clock = at
		if err := store.ReconcileNodeIdentities(map[string]string{"node-a": "uid-a"}, at); err != nil {
			t.Fatal(err)
		}
		o := contextSample(at)
		value := uint64(0)
		if at.Equal(now) {
			value = 100
		}
		o.Stats.Memory.WorkingSetBytes = &value
		if err := store.ReplaceNodeContext(o); err != nil {
			t.Fatal(err)
		}
	}
	s := memoryhistory.Selection{Request: memoryhistory.Request{Scope: memoryhistory.Node, Name: "node-a"}, UID: "uid-a", ResolvedAt: now, Targets: []memoryhistory.Target{{Node: "node-a", NodeUID: "uid-a", StartedAt: now.Add(-time.Hour)}}}
	q := memoryhistory.Query{Source: memoryhistory.Local, Metric: memoryhistory.WorkingSet, Start: now.Add(-2 * time.Minute), End: now, Step: time.Minute}
	r, err := store.MemoryHistory().Query(t.Context(), s, q)
	if err != nil {
		t.Fatal(err)
	}
	points := r.Series[0].Points
	if points[0].Bytes == nil || *points[0].Bytes != 0 || points[1].Bytes != nil || points[2].Bytes == nil || *points[2].Bytes != 100 {
		t.Fatal("Node gaps or zero were lost")
	}
	if r.Series[0].Origin != "kubelet-summary" || r.Series[0].FreshFor != nodecontext.StaleAfter {
		t.Fatal("Node source semantics changed")
	}
	if err := r.Validate(); err != nil {
		t.Fatalf("invalid local Node contract: %v", err)
	}
	s.UID, s.Targets[0].NodeUID = "uid-b", "uid-b"
	r, err = store.MemoryHistory().Query(t.Context(), s, q)
	if err != nil || r.State != memoryhistory.Missing {
		t.Fatal("Node name reuse returned earlier instance data")
	}
}

func TestLocalNodeHistoryReportsDisabledSource(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	s := memoryhistory.Selection{Request: memoryhistory.Request{Scope: memoryhistory.Node, Name: "node-a"}, UID: "uid-a", ResolvedAt: now, Targets: []memoryhistory.Target{{Node: "node-a", NodeUID: "uid-a", StartedAt: now.Add(-time.Hour)}}}
	q := memoryhistory.Query{Source: memoryhistory.Local, Metric: memoryhistory.WorkingSet, Start: now.Add(-time.Minute), End: now, Step: time.Minute}
	r, err := NewStore().MemoryHistory().Query(t.Context(), s, q)
	if err != nil || r.State != memoryhistory.Disabled || len(r.Series) != 0 {
		t.Fatalf("disabled source: %+v %v", r, err)
	}
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
}
