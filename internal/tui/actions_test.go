package tui

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/incident"
	"github.com/danushkastanley/kube-memlens/internal/model"
)

func TestRecommendationActionReusesReadOnlyDomainRecommendations(t *testing.T) {
	pod := actionFixturePod()
	result, err := (localActionExecutor{}).Run(context.Background(), actionRequest{
		kind: actionRecommend,
		ref:  entityRef{kind: entityPod, namespace: pod.Namespace, podName: pod.PodName},
		pods: []api.PodSnapshot{pod},
	})
	if err != nil {
		t.Fatalf("recommendation action: %v", err)
	}
	joined := strings.Join(result.lines, "\n")
	for _, want := range []string{"Automatic mutation: disabled", "investigate-oom-evidence", "Recent OOM"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("recommendation missing %q:\n%s", want, joined)
		}
	}
}

func TestCompareActionShowsCompositionDeltas(t *testing.T) {
	before := actionFixturePod()
	after := before
	after.PodName = "after"
	after.Memory.TotalBytes += 64 << 20
	result, err := (localActionExecutor{}).Run(context.Background(), actionRequest{kind: actionCompare, before: &before, after: &after})
	if err != nil {
		t.Fatalf("compare action: %v", err)
	}
	joined := strings.Join(result.lines, "\n")
	for _, want := range []string{"Before: default/api", "After:  default/after", "Total", "+64Mi", "Diagnosis:", "Before observation:", "Before counters:", "After observation:", "After counters:"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("comparison missing %q:\n%s", want, joined)
		}
	}
}

func TestCaptureActionWritesPrivateRedactedFileAndRequiresOverwrite(t *testing.T) {
	pod := actionFixturePod()
	path := filepath.Join(t.TempDir(), "incident.json")
	request := actionRequest{
		kind:       actionCapture,
		ref:        entityRef{kind: entityPod, namespace: pod.Namespace, podName: pod.PodName},
		pods:       []api.PodSnapshot{pod},
		histories:  []api.PodHistory{{Namespace: pod.Namespace, PodName: pod.PodName, PodUID: "sensitive-uid"}},
		outputPath: path,
	}
	result, err := (localActionExecutor{}).Run(context.Background(), request)
	if err != nil {
		t.Fatalf("capture action: %v", err)
	}
	if result.outputPath != path {
		t.Fatalf("capture path = %q", result.outputPath)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat capture: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("capture mode = %o", info.Mode().Perm())
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read capture: %v", err)
	}
	for _, forbidden := range []string{"sensitive-uid", "container-secret", "/sensitive/cgroup", "customer-secret"} {
		if strings.Contains(string(body), forbidden) {
			t.Fatalf("capture contains %q", forbidden)
		}
	}

	result, err = (localActionExecutor{}).Run(context.Background(), request)
	var exists incident.ExistsError
	if !errors.As(err, &exists) || !result.overwriteRequired {
		t.Fatalf("second capture result=%#v err=%v", result, err)
	}
}

func TestCaptureActionFiltersInjectedHistoryAndNodeDataToSelectedPod(t *testing.T) {
	pod := actionFixturePod()
	pod.NodeName = "node-a"
	path := filepath.Join(t.TempDir(), "incident.json")
	request := actionRequest{
		kind: actionCapture,
		ref:  entityRef{kind: entityPod, namespace: pod.Namespace, podName: pod.PodName},
		pods: []api.PodSnapshot{pod},
		nodes: []api.NodeSnapshotStatus{
			{NodeName: "node-a"},
			{NodeName: "node-b"},
		},
		histories: []api.PodHistory{
			{Namespace: pod.Namespace, PodName: pod.PodName, PodUID: pod.PodUID},
			{Namespace: "other-tenant", PodName: pod.PodName, PodUID: pod.PodUID},
			{Namespace: pod.Namespace, PodName: "other-pod", PodUID: "other-uid"},
		},
		outputPath: path,
	}
	if _, err := (localActionExecutor{}).Run(context.Background(), request); err != nil {
		t.Fatalf("capture action: %v", err)
	}
	var bundle api.IncidentBundle
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read capture: %v", err)
	}
	if err := json.Unmarshal(body, &bundle); err != nil {
		t.Fatalf("decode capture: %v", err)
	}
	if len(bundle.Nodes) != 1 || bundle.Nodes[0].NodeName != "node-a" || len(bundle.Histories) != 1 {
		t.Fatalf("capture widened scope: nodes=%#v histories=%#v", bundle.Nodes, bundle.Histories)
	}

	request.outputPath = filepath.Join(t.TempDir(), "partial.json")
	request.partial = true
	if _, err := (localActionExecutor{}).Run(context.Background(), request); err != nil {
		t.Fatalf("partial capture action: %v", err)
	}
	body, err = os.ReadFile(request.outputPath)
	if err != nil {
		t.Fatalf("read partial capture: %v", err)
	}
	if err := json.Unmarshal(body, &bundle); err != nil {
		t.Fatalf("decode partial capture: %v", err)
	}
	if len(bundle.Nodes) != 0 {
		t.Fatalf("partial capture contains cluster nodes: %#v", bundle.Nodes)
	}
}

func TestOSC52SequenceContainsOnlyEncodedSafeCommand(t *testing.T) {
	command := "kubectl memlens explain pod api -n default"
	sequence := osc52Sequence(command)
	if !strings.HasPrefix(sequence, "\x1b]52;c;") || !strings.HasSuffix(sequence, "\a") {
		t.Fatalf("OSC 52 sequence = %q", sequence)
	}
	encoded := strings.TrimSuffix(strings.TrimPrefix(sequence, "\x1b]52;c;"), "\a")
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || string(decoded) != command {
		t.Fatalf("decoded command = %q err=%v", decoded, err)
	}
}

func TestCaptureCancellationWritesNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cancelled.json")
	m := newModel(context.Background(), Options{}, nil, "test")
	m.action.mode = actionCapturePath
	m.action.input = path
	updated, command := m.handleActionKey(keyMessage("esc"))
	m = updated.(appModel)
	if command != nil || m.action.mode != actionClosed {
		t.Fatalf("cancel state=%v command=%v", m.action.mode, command)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("cancelled capture wrote a file: %v", err)
	}
}

func TestStaleActionResponseCannotReplaceCurrentResult(t *testing.T) {
	m := newModel(context.Background(), Options{}, nil, "test")
	m.action.activeID = 2
	m.action.result = actionResult{title: "current"}
	m.completeAction(actionMsg{id: 1, result: actionResult{title: "stale"}})
	if m.action.result.title != "current" {
		t.Fatalf("stale action replaced result: %#v", m.action.result)
	}
	m.completeAction(actionMsg{id: 2, err: errors.New("failed")})
	if m.action.err == nil || m.action.inFlight {
		t.Fatalf("current action failure state = %#v", m.action)
	}
}

func actionFixturePod() api.PodSnapshot {
	container := api.ContainerSnapshot{
		Namespace: "default", PodName: "api", PodUID: "sensitive-uid", ContainerName: "app",
		ContainerID: "container-secret", CgroupPath: "/sensitive/cgroup", CapturedAt: time.Now(),
		Context: api.ContainerContext{Labels: map[string]string{"customer": "customer-secret"}},
		Memory:  model.MemoryBreakdown{TotalBytes: 128 << 20, AnonBytes: 96 << 20, EventDeltasKnown: true, OOMKillEventsDelta: 1},
	}
	return api.PodSnapshot{
		Namespace: "default", PodName: "api", PodUID: "sensitive-uid", CapturedAt: container.CapturedAt,
		Context:    api.PodContext{Labels: map[string]string{"customer": "customer-secret"}},
		Containers: []api.ContainerSnapshot{container}, Memory: container.Memory,
	}
}
