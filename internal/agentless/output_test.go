package agentless

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/observation"
	corev1 "k8s.io/api/core/v1"
)

func TestOutputBudgetMatchesEncodedSizeWithoutEncodingWholeFrame(t *testing.T) {
	reader, _ := newCurrentFixture(t, Options{})
	batch, err := reader.Current(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []observation.Batch{batch, {}, {Pods: []observation.Pod{}, Nodes: []observation.Node{}}} {
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if err := checkOutputSize(value, int64(len(data))); err != nil {
			t.Fatalf("rejected exact size: %v", err)
		}
		if err := checkOutputSize(value, int64(len(data)-1)); err == nil {
			t.Fatal("accepted oversized frame")
		}
	}
}

func TestPodTextCannotAmplifyUnboundedMetadataIntoContainers(t *testing.T) {
	pod := pendingPod("team-a", "app")
	large := strings.Repeat("x", 1<<20)
	pod.Spec.RuntimeClassName = &large
	pod.Status.QOSClass = corev1.PodQOSClass(large)
	row, err := podMetadata(t.Context(), pod, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(row.Containers[0].Context.RuntimeClassName) != 253 || len(row.Containers[0].Context.QoSClass) != 64 {
		t.Fatal("copied text is not bounded")
	}
	for range 1000 {
		row.Containers = append(row.Containers, row.Containers[0])
	}
	if err := checkOutputSize(observation.Batch{Pods: []observation.Pod{row}}, 1024); err == nil {
		t.Fatal("large container result passed output bound")
	}
}
