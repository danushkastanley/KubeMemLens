package metrics

import (
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/metrics/expositiontest"
)

func renderFormatForTest(t *testing.T, format Format) string {
	t.Helper()
	out, err := (Exporter{
		Source: testSource{}, TTL: time.Minute, Now: fixedNow, Opts: DefaultOptions(), Format: format,
		Operational: &Operational{Health: &HealthOperational{}},
	}).Render()
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestOpenMetricsCounterFamiliesOmitTheTotalSuffix(t *testing.T) {
	out := renderFormatForTest(t, OpenMetrics)
	for _, problem := range expositiontest.CounterFamilyProblems(out) {
		t.Error(problem)
	}
	// One counter from each writer, so the check above cannot pass on no counters.
	assertContains(t, out, "# TYPE kubememlens_collector_history_loss counter\n")
	assertContains(t, out, "# HELP kubememlens_volume_usage_commits ")
	assertContains(t, out, "# TYPE kubememlens_volume_usage_commits counter\nkubememlens_volume_usage_commits_total 0\n")
}

func TestPrometheusTextCounterFamiliesKeepTheSampleName(t *testing.T) {
	// The classic text format has no families: TYPE must name the sample itself.
	out := renderFormatForTest(t, PrometheusText)
	assertContains(t, out, "# TYPE kubememlens_collector_history_loss_total counter\n")
	assertContains(t, out, "# HELP kubememlens_volume_usage_commits_total ")
	assertContains(t, out, "# TYPE kubememlens_volume_usage_commits_total counter\nkubememlens_volume_usage_commits_total 0\n")
}
