package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/model"
)

func TestMemoryQoSTerminalDetailAndSelectedActionScope(t *testing.T) {
	pod := actionFixturePod()
	pod.Containers[0].Memory = model.MemoryBreakdown{MinKnown: true, LowKnown: true, HighKnown: true, HighUnlimited: true, MaxKnown: true, MaxUnlimited: true}
	pod.Containers[0].CapturedAt = time.Now().UTC()
	m := newModel(context.Background(), Options{}, nil, "test")
	m.data.Pods = []api.PodSnapshot{pod}
	m.detail = entityRef{kind: entityPod, namespace: pod.Namespace, podName: pod.PodName}
	output := strings.Join(m.detailLines(100), "\n")
	if !strings.Contains(output, "Reclaim protection memory.min: 0B") || !strings.Contains(output, "Throttle boundary memory.high: unlimited") {
		t.Fatal(output)
	}
	selected := qosPodsForRef(entityRef{kind: entityContainer, namespace: pod.Namespace, podName: pod.PodName, containerName: pod.Containers[0].ContainerName}, []api.PodSnapshot{pod})
	if len(selected) != 1 || len(selected[0].Containers) != 1 {
		t.Fatal(selected)
	}
	if got := qosPodsForRef(entityRef{kind: entityPod, namespace: "other", podName: pod.PodName}, []api.PodSnapshot{pod}); len(got) != 0 {
		t.Fatal("QoS action escaped selected namespace")
	}
}
