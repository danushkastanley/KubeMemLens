package tui

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/model"
)

func TestResourceContextThroughTerminalDetailCompareAndCapture(t *testing.T) {
	pod := actionFixturePod()
	pod.Context.Resources = model.PodMemoryResources{
		Configured: model.MemoryResourceBudget{Limit: model.ResourceValue{Known: true, Bytes: 384 << 20}},
		Pending:    model.ResizeObservation{State: model.ResizeDeferred, Source: model.ResizePodCondition},
	}
	pod.Containers[0].Context.Resources = model.ContainerMemoryResources{Pod: pod.Context.Resources, AllocatedRequest: model.ResourceValue{Known: true, Bytes: 64 << 20}}
	m := newModel(context.Background(), Options{}, nil, "test")
	m.data.Pods = []api.PodSnapshot{pod}
	m.detail = entityRef{kind: entityPod, namespace: pod.Namespace, podName: pod.PodName}
	lines := strings.Join(m.detailLines(100), "\n")
	for _, want := range []string{"384Mi (Pod configuration)", "Resize allocation: deferred", "Kubelet allocated request: 64Mi"} {
		if !strings.Contains(lines, want) {
			t.Fatalf("missing %q in %s", want, lines)
		}
	}
	before := api.LegacyPodSnapshot(pod)
	result, err := compareResult(actionRequest{before: &before, after: &pod})
	if err != nil || !strings.Contains(strings.Join(result.lines, "\n"), "Pod configured limit: not reported -> 384Mi") {
		t.Fatalf("comparison = %+v, %v", result, err)
	}
	path := filepath.Join(t.TempDir(), "capture.json")
	_, err = captureResult(actionRequest{ref: m.detail, pods: []api.PodSnapshot{pod}, outputPath: path})
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var bundle api.IncidentBundle
	if err := json.Unmarshal(body, &bundle); err != nil {
		t.Fatal(err)
	}
	if bundle.SchemaVersion != 2 || bundle.Pods[0].Context.Resources != pod.Context.Resources {
		t.Fatal("terminal capture lost resource context")
	}
}
