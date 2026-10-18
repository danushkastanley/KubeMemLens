package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/model"
	"github.com/danushkastanley/kube-memlens/internal/profiler"
)

func TestRecommendationHandoffUsesSelectedContainerOnly(t *testing.T) {
	now := time.Now().UTC()
	c := api.ContainerSnapshot{Namespace: "test", PodName: "app-1", PodUID: "uid", ContainerName: "app", ContainerID: "id", CapturedAt: now,
		Freshness: api.EvidenceFreshnessFresh, Completeness: api.EvidenceComplete, Memory: model.MemoryBreakdown{TotalBytes: 100 << 20, AnonBytes: 80 << 20},
		Context: api.ContainerContext{PodPhase: "Running", Labels: map[string]string{profiler.LabelPrefix + "app": profiler.GoHeapProfile}}}
	sidecar := c
	sidecar.ContainerName = "sidecar"
	sidecar.ContainerID = "sidecar-id"
	p := api.PodSnapshot{Namespace: c.Namespace, PodName: c.PodName, PodUID: c.PodUID, CapturedAt: now, Freshness: c.Freshness, Completeness: c.Completeness,
		Context: api.PodContext{Phase: "Running"}, Containers: []api.ContainerSnapshot{c, sidecar}, Memory: c.Memory}
	request := actionRequest{ref: entityRef{kind: entityContainer, namespace: p.Namespace, podName: p.PodName, containerName: "app"}, pods: []api.PodSnapshot{p}}
	result, err := recommendationResult(request)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(result.lines, "\n"), "go tool pprof -top") {
		t.Fatal("declared handoff absent")
	}
	request.ref.containerName = "sidecar"
	result, err = recommendationResult(request)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(result.lines, "\n")
	if strings.Contains(joined, "go tool pprof") || !strings.Contains(joined, "No explicit supported runtime") {
		t.Fatal("sidecar inherited application's declaration")
	}
}
