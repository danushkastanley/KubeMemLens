package traceview

import (
	"strings"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	"github.com/danushkastanley/kube-memlens/internal/traceaggregate"
	"github.com/danushkastanley/kube-memlens/internal/traceframe"
)

func TestNumericSummaryRetainsOperationAndMissingEvidenceMeaning(t *testing.T) {
	pages := uint64(4)
	cases := []struct {
		aggregate traceaggregate.Summary
		want      []string
	}{
		{traceaggregate.Summary{Kind: trace.Files, Reads: traceaggregate.FileOperations{Operations: 3, CompletedBytes: traceaggregate.Total{Unreported: true}}}, []string{"file reads: 3; writes: 0", "completed: unreported"}},
		{traceaggregate.Summary{Kind: trace.Cache, Additions: traceaggregate.CacheOperations{Operations: 2, Pages: traceaggregate.Total{Value: &pages}}}, []string{"cache additions: 2; removals: 0", "pages added: 4"}},
		{traceaggregate.Summary{Kind: trace.OOM, OOM: traceaggregate.OOMCounts{Cgroup: 1, Unknown: 2, MissingProcessContext: 2}}, []string{"OOM decisions: cgroup 1; global 0; unknown 2", "decisions missing process context: 2"}},
	}
	for _, test := range cases {
		text := strings.Join(SummaryLines(traceframe.Summary{Termination: trace.EventLimit, Incomplete: true, Aggregates: &test.aggregate}), "\n")
		for _, want := range append(test.want, "incomplete evidence: true", "engine produced: unreported", "lost: unreported") {
			if !strings.Contains(text, want) {
				t.Fatalf("missing %q in %s", want, text)
			}
		}
	}
	if text := total(traceaggregate.Total{Unreported: true, Overflow: true}); text != "unreported; overflow" {
		t.Fatal("overflow presented as an exact value")
	}
}
