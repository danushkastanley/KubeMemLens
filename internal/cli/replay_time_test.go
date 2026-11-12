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
	"github.com/danushkastanley/kube-memlens/internal/model"
)

// A capture is replayed long after it was taken, so its evidence must be read
// against the capture time, not the time of the replay.
var capturedLongAgo = time.Date(2026, time.January, 10, 12, 0, 0, 0, time.UTC)

func oomKilledBeforeCapture(name string, capturedAt time.Time) api.PodSnapshot {
	memory := model.MemoryBreakdown{TotalBytes: 64 << 20, AnonBytes: 8 << 20, FileBytes: 8 << 20, EventDeltasKnown: true}
	return api.PodSnapshot{
		Namespace: "shop", PodName: name, PodUID: "uid-" + name, NodeName: "node-a", CapturedAt: capturedAt.Add(-30 * time.Second),
		Freshness: api.EvidenceFreshnessFresh, Completeness: api.EvidenceComplete, Memory: memory,
		Containers: []api.ContainerSnapshot{{
			Namespace: "shop", PodName: name, PodUID: "uid-" + name, NodeName: "node-a", ContainerName: "app", ContainerID: "id-" + name,
			CapturedAt: capturedAt.Add(-30 * time.Second), Memory: memory,
		}},
		Context: api.PodContext{
			Phase: "Running", RestartCount: 1, CreatedAt: capturedLongAgo.Add(-time.Hour),
			LastTerminationKnown: true, LastTerminationReason: "OOMKilled", LastTerminationExitCode: 137,
			LastTerminationFinishedAt: capturedLongAgo.Add(-2 * time.Minute),
		},
	}
}

func writeDeepBundle(t *testing.T, name string, capturedAt time.Time, pods ...api.PodSnapshot) string {
	t.Helper()
	body, err := json.Marshal(api.IncidentBundle{SchemaVersion: 1, CapturedAt: capturedAt, ToolVersion: "test", Pods: pods})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestReplayDiagnosesThePodAtTheCaptureTime(t *testing.T) {
	path := writeDeepBundle(t, "capture.json", capturedLongAgo, oomKilledBeforeCapture("checkout", capturedLongAgo))
	var output bytes.Buffer
	command := newReplayCommand()
	command.SetOut(&output)
	command.SetArgs([]string{path})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Incident captured: 2026-01-10T12:00:00Z",
		"oom-risk",
		"Severity: critical",
		"termination occurred 2m0s ago",
		"created: 2026-01-10T11:00:00Z (1h0m0s before capture)",
	} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("replay missing %q:\n%s", want, output.String())
		}
	}
}

func TestReplayTableAgesAreRelativeToTheCapture(t *testing.T) {
	path := writeDeepBundle(t, "capture.json", capturedLongAgo,
		oomKilledBeforeCapture("checkout", capturedLongAgo), oomKilledBeforeCapture("basket", capturedLongAgo))
	var output bytes.Buffer
	command := newReplayCommand()
	command.SetOut(&output)
	command.SetArgs([]string{path})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(output.String(), "\n") {
		if strings.Contains(line, "checkout") && !(strings.Contains(line, " 1h ") && strings.Contains(line, " 30s ")) {
			t.Fatalf("Pod and sample ages are not relative to the capture: %q", line)
		}
	}
	if !strings.Contains(output.String(), "checkout") {
		t.Fatalf("table missing the Pod:\n%s", output.String())
	}
}

func TestIncidentComparisonDiagnosesEachSideAtItsOwnCaptureTime(t *testing.T) {
	later := capturedLongAgo.Add(time.Hour)
	before := writeDeepBundle(t, "before.json", capturedLongAgo, oomKilledBeforeCapture("checkout", capturedLongAgo))
	after := writeDeepBundle(t, "after.json", later, oomKilledBeforeCapture("checkout", later))
	var output bytes.Buffer
	command := newCompareCommand(nil)
	command.SetOut(&output)
	command.SetArgs([]string{"--before", before, "--after", after, "--pod", "shop/checkout"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "Diagnosis: oom-risk (high) → normal (low)") {
		t.Fatalf("comparison did not diagnose each capture at its own time:\n%s", output.String())
	}
}

// Bundles written without a capture time fall back to the Pod's sample time,
// and claim no age at all when that is missing too.
func TestReplayWithoutACaptureTimeClaimsNoAge(t *testing.T) {
	pod := oomKilledBeforeCapture("checkout", capturedLongAgo)
	pod.CapturedAt, pod.Containers[0].CapturedAt = time.Time{}, time.Time{}
	path := writeDeepBundle(t, "capture.json", time.Time{}, pod)
	var output bytes.Buffer
	command := newReplayCommand()
	command.SetOut(&output)
	command.SetArgs([]string{path})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	text := output.String()
	if !strings.Contains(text, "Incident captured: unknown") || !strings.Contains(text, "created: 2026-01-10T11:00:00Z\n") {
		t.Fatalf("replay did not state that the capture time is unknown:\n%s", text)
	}
	if strings.Contains(text, "before capture") || strings.Contains(text, "oom-risk") {
		t.Fatalf("replay claimed an age or recency without a capture time:\n%s", text)
	}
}
