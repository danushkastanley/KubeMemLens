package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/explain"
	"github.com/danushkastanley/kube-memlens/internal/model"
)

func qosCLIPod() api.PodSnapshot {
	now := time.Now().UTC()
	c := api.ContainerSnapshot{Namespace: "tenant-a", PodName: "app", ContainerName: "worker", ContainerID: "private-container-id", CgroupPath: "/private-path", CapturedAt: now, Freshness: api.EvidenceFreshnessFresh, DeltaStartedAt: now.Add(-5 * time.Second), DeltaWindowKnown: true,
		Memory:  model.MemoryBreakdown{TotalBytes: 16 << 20, MinKnown: true, LowKnown: true, HighKnown: true, HighUnlimited: true, MaxKnown: true, MaxBytes: 128 << 20, LocalEventsKnown: true, LocalEventDeltasKnown: true, PressureKnown: true},
		Context: api.ContainerContext{MemoryRequestKnown: true, MemoryRequestBytes: 32 << 20, MemoryLimitKnown: true, MemoryLimitBytes: 128 << 20},
	}
	return api.PodSnapshot{Namespace: c.Namespace, PodName: c.PodName, CapturedAt: now, Containers: []api.ContainerSnapshot{c}, Memory: c.Memory}
}

func TestMemoryQoSMachineAndReplayPreserveExistingEvidence(t *testing.T) {
	pod := qosCLIPod()
	document := podExplanationDocument(pod)
	if document.SchemaVersion != 3 || document.Containers[0].MemoryQoS.High.State != explain.BoundaryUnlimited {
		t.Fatalf("missing typed observation: %+v", document)
	}
	body, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "private-") {
		t.Fatal("machine QoS output leaked identifiers")
	}
	bundle := api.IncidentBundle{SchemaVersion: 1, Pods: []api.PodSnapshot{pod}}
	body, err = json.Marshal(bundle)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "old-capture.json")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	command := newReplayCommand()
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetArgs([]string{path})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Reclaim protection memory.min: 0B", "Throttle boundary memory.high: unlimited", "no-recent-crossing"} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("replay missing %q: %s", want, output.String())
		}
	}
}

func TestDoctorMemoryQoSDoesNotWarnForUnlimitedHigh(t *testing.T) {
	pod := qosCLIPod()
	var report doctorReport
	report.addMemoryQoSCheck(pod.Containers)
	if report.Checks[0].Status != "pass" || report.shouldFail(true) {
		t.Fatal(report.Checks)
	}
	pod.Containers[0].Freshness = api.EvidenceFreshnessStale
	report = doctorReport{}
	report.addMemoryQoSCheck(pod.Containers)
	if report.Checks[0].Status != "warn" || !strings.Contains(report.Checks[0].Summary, "stale=1") {
		t.Fatal(report.Checks)
	}
	pod.Containers[0].Freshness = api.EvidenceFreshnessFresh
	pod.Containers[0].Memory.LowKnown = false
	report = doctorReport{}
	report.addMemoryQoSCheck(pod.Containers)
	if !strings.Contains(report.Checks[0].Summary, "unavailable=1") {
		t.Fatal(report.Checks)
	}
}

func TestMemoryQoSComparisonShowsBoundaryChangeWithoutChargeChange(t *testing.T) {
	before := qosCLIPod()
	after := before
	after.Containers = append([]api.ContainerSnapshot(nil), before.Containers...)
	after.Containers[0].Memory.HighUnlimited = false
	after.Containers[0].Memory.HighBytes = 80 << 20
	after.Containers[0].Memory.LocalHighEventsDelta = 4
	var output bytes.Buffer
	printPodComparison(&output, "comparison", before, after, time.Minute)
	if !strings.Contains(output.String(), "throttle boundary memory.high: unlimited -> 80Mi") || !strings.Contains(output.String(), "high-event delta: 0 (memory.events.local;") || !strings.Contains(output.String(), " -> 4 (memory.events.local;") {
		t.Fatal(output.String())
	}
}
