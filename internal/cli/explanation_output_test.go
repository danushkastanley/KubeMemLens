package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/model"
)

func TestPodExplanationDocumentOmitsSensitiveRuntimeFields(t *testing.T) {
	now := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	pod := api.PodSnapshot{
		Namespace: "default", PodName: "api", PodUID: "sensitive-pod-uid", NodeName: "node-a",
		CapturedAt: now,
		Context: api.PodContext{
			Labels: map[string]string{"customer": "secret-label"}, RuntimeClassName: "gvisor",
			MemoryEmptyDirCount: 2, MemoryEmptyDirLimited: 1, MemoryEmptyDirLimitBytes: 64 << 20,
			NodeMemoryAllocatableKnown: true, NodeMemoryAllocatable: 8 << 30,
		},
		Memory: model.MemoryBreakdown{TotalBytes: 100, AnonBytes: 50, FileBytes: 20, ShmemBytes: 5, SlabReclaimableBytes: 4, SlabUnreclaimableBytes: 3, KernelBytes: 15, SocketBytes: 2, PageTableBytes: 1, FileMappedBytes: 6, AnonTHPBytes: 7, FileTHPBytes: 8, ShmemTHPBytes: 9, ReclaimDeltasKnown: true, PageScanDelta: 10, PageStealDelta: 8, RefaultFileDelta: 2},
		Containers: []api.ContainerSnapshot{{
			ContainerName: "app", ContainerID: "sensitive-container-id", CgroupPath: "/sensitive/cgroup/path",
			CapturedAt: now, DeltaStartedAt: now.Add(-10 * time.Second), DeltaWindowKnown: true,
			Context: api.ContainerContext{Labels: map[string]string{"customer": "secret-label"}},
			Memory:  model.MemoryBreakdown{TotalBytes: 100, AnonBytes: 80},
		}},
	}
	document := podExplanationDocument(pod)
	body, err := json.Marshal(document)
	if err != nil {
		t.Fatalf("marshal explanation: %v", err)
	}
	if document.SchemaVersion != api.CurrentExplanationSchemaVersion || document.Finding.Confidence == "" || document.Finding.Severity == "" || len(document.Finding.Caveats) == 0 {
		t.Fatalf("invalid explanation document: %#v", document)
	}
	if !document.Finding.EvidenceWindow.DeltaKnown || document.Finding.EvidenceWindow.DeltaStart == nil || !document.Finding.EvidenceWindow.DeltaStart.Equal(now.Add(-10*time.Second)) {
		t.Fatalf("missing exact evidence window: %#v", document.Finding.EvidenceWindow)
	}
	if document.Kubernetes.RuntimeClassName != "gvisor" || document.Kubernetes.MemoryEmptyDirCount != 2 || document.Kubernetes.NodeMemoryAllocatable != 8<<30 {
		t.Fatalf("missing Kubernetes context: %#v", document.Kubernetes)
	}
	if !document.Memory.RecentReclaim.Known || document.Memory.RecentReclaim.PageScan != 10 || document.Memory.RecentReclaim.PageSteal != 8 || document.Memory.RecentReclaim.RefaultFile != 2 {
		t.Fatalf("missing reclaim evidence: %#v", document.Memory.RecentReclaim)
	}
	if document.Memory.ResidualBytes != 30 || document.Memory.SlabReclaimableBytes != 4 || document.Memory.SlabUnreclaimableBytes != 3 || document.Memory.SocketBytes != 2 || document.Memory.PageTableBytes != 1 {
		t.Fatalf("missing memory taxonomy: %#v", document.Memory)
	}
	if document.Memory.FileMappedBytes != 6 || document.Memory.AnonTHPBytes != 7 || document.Memory.FileTHPBytes != 8 || document.Memory.ShmemTHPBytes != 9 {
		t.Fatalf("missing mapped/THP detail: %#v", document.Memory)
	}
	for _, forbidden := range []string{"sensitive-pod-uid", "sensitive-container-id", "/sensitive/cgroup/path", "secret-label", "cgroupPath", "containerID", "labels"} {
		if strings.Contains(string(body), forbidden) {
			t.Fatalf("explanation contains %q: %s", forbidden, body)
		}
	}
}

func TestWriteExplanationDocumentJSONAndYAML(t *testing.T) {
	document := explanationDocument{SchemaVersion: api.CurrentExplanationSchemaVersion, Target: explanationTarget{Kind: "Pod", Namespace: "default", Name: "api"}}
	for _, output := range []string{"json", "yaml"} {
		buffer := &bytes.Buffer{}
		if err := writeExplanationDocument(buffer, output, document); err != nil {
			t.Fatalf("write %s: %v", output, err)
		}
		if !strings.Contains(buffer.String(), "schemaVersion") || !strings.Contains(buffer.String(), "api") {
			t.Fatalf("unexpected %s output: %s", output, buffer.String())
		}
	}
}
