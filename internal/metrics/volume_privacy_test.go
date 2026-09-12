package metrics

import (
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/model"
)

func TestIOEnrichmentDoesNotChangeMetricValuesOrCardinality(t *testing.T) {
	now := fixedNow()
	source := testSource{pods: []api.PodSnapshot{{Namespace: "team", PodName: "app", CapturedAt: now, Memory: memory(300)}}, containers: []api.ContainerSnapshot{{Namespace: "team", PodName: "app", ContainerName: "app", CapturedAt: now, Memory: memory(300)}}}
	render := func() string {
		t.Helper()
		out, err := (Exporter{Source: source, TTL: time.Minute, Now: fixedNow, Opts: DefaultOptions(), MaxBytes: 1 << 20}).Render()
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	before := render()
	source.pods[0].Memory.IOPressure = model.IOPressure{State: model.IOAvailable, Some: model.PSIWindow{Avg10: 40, TotalMicros: 5000}}
	source.containers[0].Memory.IOPressure = source.pods[0].Memory.IOPressure
	if after := render(); after != before {
		t.Fatal("I/O enrichment altered default memory metrics or added labels")
	}
}
