package observationview

import (
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/capability"
	"github.com/danushkastanley/kube-memlens/internal/observation"
)

func comparisonRow(value *uint64, at time.Time) Row {
	set := observation.WorkingSet{Bytes: value, Availability: capability.Available, Coverage: observation.Coverage{Reported: 1, Expected: 1, Unit: capability.ContainerScope}, Evidence: capability.Envelope{Source: capability.KubernetesMetrics, APIVersion: "metrics.k8s.io/v1beta1", CapturedAt: at, ReceivedAt: at, Scope: capability.PodScope, Window: 15 * time.Second, Freshness: capability.Fresh, Completeness: capability.Complete}}
	return Row{Mode: capability.Restricted, Scope: capability.PodScope, Namespace: "test", Name: "pod", WorkingSet: &set, Pod: &observation.Pod{Name: "pod", UID: "original"}}
}

func TestComparisonPreservesMissingCoverageSourceAndIdentity(t *testing.T) {
	at := time.Date(2026, 9, 11, 0, 0, 0, 0, time.UTC)
	zero, value := uint64(0), uint64(32<<20)
	for name, mutate := range map[string]func(*Row){
		"missing":  func(r *Row) { r.WorkingSet.Bytes = nil },
		"coverage": func(r *Row) { r.WorkingSet.Coverage.Expected = 2 },
		"api":      func(r *Row) { r.WorkingSet.Evidence.APIVersion = "metrics.k8s.io/v1" },
	} {
		t.Run(name, func(t *testing.T) {
			before, after := comparisonRow(&zero, at), comparisonRow(&value, at)
			mutate(&after)
			lines, err := Compare(before, after, at, at)
			if err != nil || !strings.Contains(strings.Join(lines, "\n"), "Delta unavailable") {
				t.Fatalf("incompatible evidence compared: %v %v", lines, err)
			}
		})
	}
	before, after := comparisonRow(&value, at), comparisonRow(&zero, at)
	after.Pod.UID = "recreated"
	lines, err := Compare(before, after, at, at)
	text := strings.Join(lines, "\n")
	if err != nil || !strings.Contains(text, "delta: -32Mi") || !strings.Contains(text, "Different object identities") {
		t.Fatalf("measured zero or recreation lost: %s %v", text, err)
	}
	after.Mode = capability.Deep
	if _, err := Compare(before, after, at, at); err == nil {
		t.Fatal("cgroup and working set comparison accepted")
	}
}

func TestComparisonReportsContainerSampleWindowsAndStaleState(t *testing.T) {
	at := time.Date(2026, 9, 11, 0, 0, 0, 0, time.UTC)
	v := uint64(10)
	before, after := comparisonRow(&v, at), comparisonRow(&v, at)
	before.Pod.Containers = []observation.Container{{WorkingSet: *before.WorkingSet}}
	after.Pod.Containers = []observation.Container{{WorkingSet: *after.WorkingSet}}
	after.Pod.Containers[0].WorkingSet.Evidence.Window = 30 * time.Second
	lines, err := Compare(before, after, at.Add(time.Hour), at.Add(time.Hour))
	text := strings.Join(lines, "\n")
	if err != nil || !strings.Contains(text, "15s → 30s") || !strings.Contains(text, "windows differ") || !strings.Contains(text, "stale → stale") {
		t.Fatalf("window or freshness lost: %s %v", text, err)
	}
}
