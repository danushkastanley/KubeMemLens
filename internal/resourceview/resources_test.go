package resourceview

import (
	"strings"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/model"
)

func TestResourcePresentationSeparatesSourcesAndUnknownValues(t *testing.T) {
	if len(PodLines(api.PodSnapshot{})) != 0 || len(ContainerLines(api.ContainerSnapshot{})) != 0 {
		t.Fatal("legacy detail gained resource extension rows")
	}
	pod := api.PodSnapshot{Context: api.PodContext{Resources: model.PodMemoryResources{
		Configured: model.MemoryResourceBudget{Request: model.ResourceValue{Bytes: 192 << 20, Known: true}, Limit: model.ResourceValue{Bytes: 384 << 20, Known: true}},
		Generation: 4, ObservedGeneration: 3,
		Pending:  model.ResizeObservation{State: model.ResizeDeferred, Source: model.ResizePodCondition, ObservedGeneration: 4},
		Applying: model.ResizeObservation{State: model.ResizeApplying, Source: model.ResizePodCondition, ObservedGeneration: 3},
	}}}
	text := strings.Join(PodLines(pod), "\n")
	for _, expected := range []string{
		model.FormatCompactBytes(192<<20) + " (Pod configuration)",
		model.FormatCompactBytes(384<<20) + " (Pod configuration)",
		"Kubelet applied Pod limit:     not reported",
		"Resize allocation: deferred (generation 4)",
		"Resize application: in-progress (generation 3)",
		"Pod spec generation: 4; kubelet observed: 3",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("missing %q in %s", expected, text)
		}
	}
	pod.Context.Resources.Pending.State = "\x1b[31mprivate"
	if text := strings.Join(PodLines(pod), "\n"); strings.Contains(text, "private") || !strings.Contains(text, "Resize allocation: unknown") {
		t.Fatal("unknown resize state was printed raw or treated as healthy")
	}
}

func TestComparisonReportsResourceChangesWithUnchangedCharge(t *testing.T) {
	before := api.PodSnapshot{Memory: model.MemoryBreakdown{TotalBytes: 128}, Context: api.PodContext{Resources: model.PodMemoryResources{
		Configured: model.MemoryResourceBudget{Limit: model.ResourceValue{Bytes: 256, Known: true}},
	}}}
	after := before
	after.Context.Resources.Configured.Limit.Bytes = 384
	after.Context.Resources.Pending = model.ResizeObservation{State: model.ResizeInfeasible, Source: model.ResizePodCondition}
	text := strings.Join(ComparisonLines(before, after), "\n")
	if !strings.Contains(text, "Pod configured limit:") || !strings.Contains(text, "Resize allocation: not reported -> infeasible") {
		t.Fatalf("resource-only change disappeared: %s", text)
	}
	if len(ComparisonLines(before, before)) != 0 {
		t.Fatal("unchanged resources produced a change")
	}
}
