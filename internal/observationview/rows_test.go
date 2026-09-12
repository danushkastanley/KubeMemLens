package observationview

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/capability"
	"github.com/danushkastanley/kube-memlens/internal/model"
	"github.com/danushkastanley/kube-memlens/internal/observation"
)

func TestRowsKeepSourcesSeparateAndMissingValuesLast(t *testing.T) {
	zero, large := uint64(0), uint64(100)
	batch := observation.Batch{Mode: capability.Restricted, Pods: []observation.Pod{
		{Namespace: "team-a", Name: "missing"},
		{Namespace: "team-a", Name: "zero", WorkingSet: observation.WorkingSet{Bytes: &zero}},
		{Namespace: "team-a", Name: "large", WorkingSet: observation.WorkingSet{Bytes: &large}},
	}}
	rows := Rows(batch)
	Sort(rows, ByMemory)
	if rows[0].Name != "large" || rows[1].Name != "zero" || rows[2].Name != "missing" {
		t.Fatal("measurement ordering changed")
	}
	encoded, err := json.Marshal(rows)
	if err != nil || strings.Contains(string(encoded), "totalBytes") || strings.Contains(string(encoded), "anonBytes") {
		t.Fatal("restricted rows gained cgroup fields")
	}
	deep := observation.Batch{Mode: capability.Deep, Pods: []observation.Pod{{Namespace: "team-a", Name: "deep", Cgroup: &observation.Cgroup{Memory: model.MemoryBreakdown{TotalBytes: 200}}}}}
	row := Rows(deep)[0]
	if row.WorkingSet != nil || row.Bytes() == nil || *row.Bytes() != 200 {
		t.Fatal("deep evidence became working set")
	}
}

func TestRowsPreserveDrillIdentityWithoutExportingLabels(t *testing.T) {
	batch := observation.Batch{Mode: capability.Restricted, Pods: []observation.Pod{{Namespace: "team-a", Name: "app", NodeName: "node-a",
		Context:    api.PodContext{WorkloadKind: "Deployment", WorkloadName: "api", Labels: map[string]string{"private-label": "private-value"}},
		Containers: []observation.Container{{Name: "worker"}},
	}}}
	rows := Rows(batch)
	if len(rows) != 2 || rows[1].Key() != "container/team-a/app/worker" || rows[1].PodName != "app" || rows[1].WorkloadName != "api" || !rows[1].Matches("NODE-A") {
		t.Fatal("drill identity lost")
	}
	encoded, _ := json.Marshal(rows)
	if strings.Contains(string(encoded), "private-label") || strings.Contains(string(encoded), "private-value") {
		t.Fatal("top output exported labels")
	}
}
