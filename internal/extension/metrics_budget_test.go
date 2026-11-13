package extension

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/collector"
)

// Between the size the renderer can fit and the entity ceiling derived from
// the response limit, the scrape used to fail whole with 507 and lose the
// collector state series that alerting depends on.
func TestMetricsDegradeByLevelInsteadOfFailingTheScrape(t *testing.T) {
	const pods = 1500
	now := time.Unix(1_800_000_000, 0).UTC()
	store := collector.NewStore()
	containers := make([]api.ContainerSnapshot, 0, pods)
	for index := range pods {
		container := readContainer(fmt.Sprintf("team-%02d", index%20), fmt.Sprintf("uid-%04d", index), fmt.Sprintf("container-%04d", index), now)
		container.PodName = fmt.Sprintf("payments-reconciliation-worker-%04d-%s", index, strings.Repeat("x", 120))
		container.NodeName = "ip-10-0-113-204.eu-west-2.compute.internal"
		containers = append(containers, container)
	}
	if _, err := store.ReplaceNodeSnapshot(api.AgentSnapshot{
		NodeName: "ip-10-0-113-204.eu-west-2.compute.internal", CapturedAt: now, Containers: containers,
	}); err != nil {
		t.Fatalf("ReplaceNodeSnapshot: %v", err)
	}
	handler := NewReadHandler(store, collector.DefaultHandlerOptions(time.Minute))
	handler.now = func() time.Time { return now }

	response := serveMetricsWithAccept(t, handler, prometheus3Accept, true)
	body := response.Body.String()
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %.300s", response.Code, body)
	}
	for _, want := range []string{
		`kubememlens_metrics_dropped_entities{level="pod",reason="response_budget_exceeded"} 1500`,
		`kubememlens_namespace_memory_bytes{namespace="team-00"`,
		"kubememlens_collector_state{",
		`kubememlens_collector_store_entities{kind="pods"} 1500`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("scrape is missing %q", want)
		}
	}
	if strings.Contains(body, "kubememlens_pod_memory_bytes{") || !strings.HasSuffix(body, "# EOF\n") {
		t.Fatalf("scrape kept Pod series or lost its terminator: %d bytes", len(body))
	}

	if json := serveMetricsWithAccept(t, handler, "application/json", true); json.Code != http.StatusOK {
		t.Fatalf("JSON status = %d, want 200: %.300s", json.Code, json.Body.String())
	}
}
