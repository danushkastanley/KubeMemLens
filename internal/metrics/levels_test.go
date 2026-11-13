package metrics

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
)

const (
	droppedType        = "# TYPE kubememlens_metrics_dropped_entities gauge"
	podBudgetDrop      = `kubememlens_metrics_dropped_entities{level="pod",reason="response_budget_exceeded"} 40`
	namespaceBudgetCut = `kubememlens_metrics_dropped_entities{level="namespace",reason="response_budget_exceeded"} 4`
)

func TestRenderDropsEntityLevelsThatExceedTheResponseBudget(t *testing.T) {
	source := budgetSource(4, 40)
	operational := &Operational{UsageEnabled: true}
	full, err := (Exporter{Source: source, TTL: time.Minute, Now: fixedNow, Opts: DefaultOptions(), Operational: operational}).Render()
	if err != nil {
		t.Fatalf("unbounded Render: %v", err)
	}
	namespaces, pods := levelBytes(full, "kubememlens_namespace_"), levelBytes(full, "kubememlens_pod_")
	base := len(full) - namespaces - pods

	tests := map[string]struct {
		maxBytes int
		want     []string
		absent   []string
	}{
		"everything fits": {
			maxBytes: len(full) + droppedFamilyReserve,
			want:     []string{"kubememlens_namespace_memory_bytes{", "kubememlens_pod_memory_bytes{"},
			absent:   []string{"response_budget_exceeded"},
		},
		"Pod level dropped": {
			maxBytes: base + namespaces + droppedFamilyReserve + pods/2,
			want:     []string{"kubememlens_namespace_memory_bytes{", podBudgetDrop},
			absent:   []string{"kubememlens_pod_", namespaceBudgetCut},
		},
		"namespace and Pod levels dropped": {
			maxBytes: base + droppedFamilyReserve + namespaces/2,
			want:     []string{podBudgetDrop, namespaceBudgetCut},
			absent:   []string{"kubememlens_pod_", "kubememlens_namespace_"},
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			out, err := (Exporter{
				Source: source, TTL: time.Minute, Now: fixedNow, Opts: DefaultOptions(),
				MaxBytes: test.maxBytes, Operational: operational,
			}).Render()
			if err != nil {
				t.Fatalf("Render with %d bytes: %v", test.maxBytes, err)
			}
			if len(out) > test.maxBytes {
				t.Fatalf("rendered %d bytes, budget is %d", len(out), test.maxBytes)
			}
			for _, needle := range append(test.want,
				`kubememlens_collector_store_entities{kind="pods"} 40`,
				"kubememlens_collector_state{",
				"kubememlens_volume_usage_enabled 1",
			) {
				assertContains(t, out, needle)
			}
			for _, needle := range test.absent {
				assertNotContains(t, out, needle)
			}
			if strings.Count(out, droppedType) != 1 || !strings.HasSuffix(out, "# EOF\n") {
				t.Fatalf("exposition is not one dropped family followed by EOF:\n%.400s", out)
			}
		})
	}
}

func TestRenderRejectsWhenCollectorSeriesAloneExceedTheBudget(t *testing.T) {
	out, err := (Exporter{Source: budgetSource(4, 40), TTL: time.Minute, Now: fixedNow, Opts: DefaultOptions(), MaxBytes: 256}).Render()
	if !errors.Is(err, ErrOutputTooLarge) || out != "" {
		t.Fatalf("Render output=%.80q error=%v", out, err)
	}
}

func budgetSource(namespaces, pods int) testSource {
	source := testSource{debug: api.DebugStore{Namespaces: namespaces, Pods: pods}}
	for index := range namespaces {
		source.namespaces = append(source.namespaces, api.NamespaceSnapshot{
			Namespace: fmt.Sprintf("team-%02d", index), Memory: memory(100),
		})
	}
	for index := range pods {
		source.pods = append(source.pods, api.PodSnapshot{
			Namespace: fmt.Sprintf("team-%02d", index%namespaces), PodName: fmt.Sprintf("api-%03d", index),
			NodeName: "node-a", Memory: memory(100),
		})
	}
	return source
}

// levelBytes is the size of every line, including HELP and TYPE, that belongs
// to the metric families with the given prefix.
func levelBytes(exposition, prefix string) int {
	total := 0
	for line := range strings.Lines(exposition) {
		if strings.HasPrefix(line, prefix) || strings.Contains(line, " "+prefix) {
			total += len(line)
		}
	}
	return total
}
