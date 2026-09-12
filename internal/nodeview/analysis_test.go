package nodeview

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/capability"
	"github.com/danushkastanley/kube-memlens/internal/nodeanalysis"
	"github.com/danushkastanley/kube-memlens/internal/nodecontext"
)

func TestNodePresentationPreservesEveryFieldAtSupportedWidths(t *testing.T) {
	now := time.Date(2026, 9, 11, 16, 0, 0, 0, time.UTC)
	zero := uint64(0)
	value := uint64(42)
	memory := &nodecontext.Memory{CapturedAt: now, UsageBytes: &zero, AvailableBytes: &value, WorkingSetBytes: &value, RSSBytes: &value, PageFaults: &value, MajorPageFaults: &zero, PSI: &nodecontext.PSI{}}
	swap := &nodecontext.Swap{CapturedAt: now, UsageBytes: &value, AvailableBytes: &zero}
	e := api.NodeEvidence{Record: api.NodeContextRecord{ReceivedAt: now, Freshness: capability.Fresh, LastGood: &nodecontext.Observation{Evidence: capability.Envelope{Completeness: capability.Partial}, Stats: &nodecontext.Stats{StartedAt: now.Add(-time.Hour), Provenance: nodecontext.Unknown}}},
		Analysis: nodeanalysis.Analysis{NodeName: "node-a", EvaluatedAt: now, Severity: nodeanalysis.Normal, Confidence: nodeanalysis.Low, ContributorAccess: nodeanalysis.ClusterPods, ObservedPodCharge: &value,
			Facts:    nodeanalysis.Facts{Source: nodecontext.Source, Availability: capability.Available, Memory: memory, Swap: swap, Context: &nodecontext.KubernetesContext{CapturedAt: now, CapacityBytes: &value, AllocatableBytes: &value, MemoryPressure: "False", Hugepages: []nodecontext.Hugepage{{Resource: "hugepages-2Mi", CapacityBytes: &value, AllocatableBytes: &zero}}}, SystemContainers: []nodecontext.SystemContainer{{Category: nodecontext.Kubelet, Memory: memory, Swap: swap}}},
			Rankings: &nodeanalysis.Rankings{Metric: nodeanalysis.Total, Pods: []nodeanalysis.Contributor{{Namespace: "team", Kind: "Pod", Name: "application", Charge: nodeanalysis.Charges{Total: 42}}}}, Caveats: []nodeanalysis.Caveat{nodeanalysis.Unqualified}}}
	before, _ := json.Marshal(e)
	for _, width := range []int{40, 80, 100, 160} {
		lines := Lines(e, now, width)
		text := strings.Join(lines, "\n")
		for _, line := range lines {
			if ansi.StringWidth(line) > width {
				t.Fatalf("line exceeds %d: %q", width, line)
			}
		}
		for _, want := range []string{"Node: node-a", "Source completeness: partial", "Usage: 0 B", "Available:", "Working set:", "RSS:", "Page faults", "Major page faults", "PSI some avg10/60/300", "PSI full cumulative stall", "Swap usage:", "Swap available:", "MemoryPressure: False", "Capacity:", "Allocatable:", "hugepages-2Mi", "kubelet", "Observed Pod charge:", "Outside observed Pods estimate:", "Unaccounted estimate:", "accounting-unqualified", "team/Pod/application"} {
			if !strings.Contains(text, want) {
				t.Fatalf("width %d missing %q", width, want)
			}
		}
	}
	after, _ := json.Marshal(e)
	if string(before) != string(after) {
		t.Fatal("presentation mutated evidence")
	}
}

func TestNodePresentationMissingAndTerminalControls(t *testing.T) {
	e := api.NodeEvidence{Analysis: nodeanalysis.Analysis{NodeName: "node\x1b[2J\u202e", ContributorAccess: nodeanalysis.NodeOnly}}
	text := strings.Join(Lines(e, time.Now(), 80), "\n")
	if strings.ContainsAny(text, "\x1b\u202e") {
		t.Fatal("terminal controls escaped sanitisation")
	}
	for _, want := range []string{"Node memory: unreported", "Node swap: unreported", "Observed Pod charge: unreported", "Contributor rankings: unavailable"} {
		if !strings.Contains(text, want) {
			t.Fatal(want)
		}
	}
}

func TestNodeHistorySortAndSinceDoNotMutate(t *testing.T) {
	now := time.Now().UTC()
	h := api.NodeContextHistory{NodeName: "node-a", Series: []api.NodeContextHistorySeries{{NodeUID: "uid", Points: []api.NodeContextHistoryPoint{{ReceivedAt: now, Observation: nodecontext.Observation{Evidence: capability.Envelope{CapturedAt: now}}}, {ReceivedAt: now.Add(-time.Minute), Observation: nodecontext.Observation{Evidence: capability.Envelope{CapturedAt: now.Add(-time.Minute)}}}}}}}
	HistoryLines(h, 80)
	filtered := HistorySince(h, now.Add(-time.Second))
	if len(filtered.Series) != 1 || len(filtered.Series[0].Points) != 1 || !h.Series[0].Points[0].ReceivedAt.Equal(now) {
		t.Fatal("history mutated or filter incorrect")
	}
}
