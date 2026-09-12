package observationview

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/capability"
	"github.com/danushkastanley/kube-memlens/internal/model"
	"github.com/danushkastanley/kube-memlens/internal/observation"
)

func TestProjectionCannotRelabelCgroupChargeAsWorkingSet(t *testing.T) {
	working := uint64(7)
	batch := observation.Batch{Mode: capability.Restricted, Pods: []observation.Pod{{Namespace: "team-a", Name: "app", WorkingSet: observation.WorkingSet{Bytes: &working}, Cgroup: &observation.Cgroup{Memory: model.MemoryBreakdown{TotalBytes: 999}}}}}
	row := Rows(batch)[0]
	if row.Cgroup != nil || row.Bytes() == nil || *row.Bytes() != 7 {
		t.Fatal("cgroup charge escaped the mode boundary")
	}
	data, _ := json.Marshal(row)
	if strings.Contains(string(data), "999") || strings.Contains(string(data), "totalBytes") {
		t.Fatal("restricted export included cgroup evidence")
	}
}

func TestSummaryDistinguishesMissingZeroStaleAndDeepEvidence(t *testing.T) {
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	zero := uint64(0)
	row := Row{Mode: capability.Restricted, Name: "app", Kind: "Pod", WorkingSet: &observation.WorkingSet{Availability: capability.Forbidden}}
	if Memory(row) != "unreported" || State(row, now) != "forbidden" {
		t.Fatal("missing metric became a healthy zero")
	}
	row.WorkingSet = &observation.WorkingSet{Bytes: &zero, Availability: capability.Available, Evidence: capability.Envelope{Source: capability.KubernetesMetrics, CapturedAt: now, ReceivedAt: now, Freshness: capability.Fresh, Completeness: capability.Complete}}
	if Memory(row) == "unreported" || State(row, now) != "fresh" || State(row, now.Add(3*time.Minute)) != "stale" {
		t.Fatal("measured zero or source age lost")
	}
	text := strings.Join(Summary(row, now), "\n")
	if !strings.Contains(text, "not cgroup charge") || !strings.Contains(text, "deep evidence is required") || strings.Contains(text, "OOM 0") {
		t.Fatal("source caveats absent")
	}
	if Age(time.Time{}, now) != "unreported" || Age(now.Add(time.Second), now) != "future" {
		t.Fatal("unknown/future age fabricated")
	}
	row.Mode = capability.Deep
	row.Cgroup = &observation.Cgroup{Memory: model.MemoryBreakdown{TotalBytes: 10}}
	if MemoryLabel(row.Mode) != "Cgroup charge" || row.Bytes() == nil || *row.Bytes() != 10 {
		t.Fatal("deep charge meaning changed")
	}
}

func TestFollowUpCommandsRejectUnsafeIdentityAndKeepMode(t *testing.T) {
	row := Row{Mode: capability.Restricted, Scope: capability.PodScope, Namespace: "team-a", Name: "app", PodName: "app"}
	if command, ok := Command(row); !ok || !strings.Contains(command, "--mode=restricted") {
		t.Fatal("missing restricted mode")
	}
	row.Mode = capability.Deep
	if command, ok := Command(row); !ok || !strings.Contains(command, "--mode=deep") {
		t.Fatal("deep command changed source")
	}
	row.Name = "app;echo injected"
	if _, ok := Command(row); ok {
		t.Fatal("unsafe name entered a command")
	}
	row.Name = "app"
	row.Scope = capability.WorkloadScope
	row.Kind = "Deployment;echo"
	if _, ok := Command(row); ok {
		t.Fatal("unsafe kind entered a command")
	}
}
