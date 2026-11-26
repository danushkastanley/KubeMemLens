package cli

import (
	"bytes"
	"fmt"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/client"
	"github.com/danushkastanley/kube-memlens/internal/collector"
	"github.com/danushkastanley/kube-memlens/internal/model"
)

// Every real name in this fixture contains "zq"; nothing a redacted capture
// may keep does.
func aliasFixtureContainer(pod, name string, now time.Time) api.ContainerSnapshot {
	return api.ContainerSnapshot{
		Namespace: "zq-shop", PodName: pod, PodUID: pod + "-uid", ContainerName: name, ContainerID: "containerd://" + pod + "-" + name,
		NodeName: "zq-node-a", CgroupPath: "/kubepods/" + pod, CapturedAt: now,
		Memory: model.MemoryBreakdown{TotalBytes: 64 << 20, AnonBytes: 32 << 20},
		Context: api.ContainerContext{QoSClass: "Burstable", PodPhase: "Running", OwnerKind: "ReplicaSet", OwnerName: "zq-web-7f9",
			WorkloadKind: "Deployment", WorkloadName: "zq-web", RuntimeClassName: "zq-sandbox", Labels: map[string]string{"team": "zq-payments"}},
	}
}

// captureAgainstCollector runs `capture` through the collector's HTTP read
// path, as an operator would, and returns what it printed.
func captureAgainstCollector(t *testing.T, args ...string) string {
	t.Helper()
	now := time.Now().UTC()
	store := collector.NewStore()
	containers := []api.ContainerSnapshot{
		aliasFixtureContainer("zq-web-7f9-abc", "zq-app", now),
		aliasFixtureContainer("zq-web-7f9-abc", "zq-proxy", now),
		aliasFixtureContainer("zq-web-7f9-def", "zq-app", now),
	}
	if _, err := store.ReplaceNodeSnapshot(api.AgentSnapshot{NodeName: "zq-node-a", CapturedAt: now, Containers: containers}); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(collector.NewReadHandlerWithOptions(store, collector.DefaultHandlerOptions(time.Minute)))
	defer server.Close()
	var output bytes.Buffer
	command := newCaptureCommand(func() client.Options {
		return client.Options{Mode: client.ConnectionModeHTTP, CollectorURL: server.URL}
	})
	command.SetOut(&output)
	command.SetErr(io.Discard)
	command.SetArgs(args)
	if err := command.Execute(); err != nil {
		t.Fatalf("capture %v: %v\n%s", args, err, output.String())
	}
	return output.String()
}

func runOffline(args ...string) (string, error) {
	var out bytes.Buffer
	command := NewRootCommand(&out, &out)
	command.SetArgs(args)
	err := command.Execute()
	return out.String(), err
}

func TestRedactedCaptureUsesAliasesForReplayAndCompare(t *testing.T) {
	dir := t.TempDir()
	before, after := filepath.Join(dir, "before.json"), filepath.Join(dir, "after.json")
	printed := captureAgainstCollector(t, "-n", "zq-shop", "--include-history", "-o", before)
	captureAgainstCollector(t, "-n", "zq-shop", "--include-history", "-o", after)
	if !strings.Contains(printed, "redacted=true") || !strings.Contains(printed, "Replay the file to list them.") || strings.Contains(printed, "zq-web-7f9-abc") {
		t.Fatalf("capture output does not explain the aliases:\n%s", printed)
	}
	data, err := os.ReadFile(before)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "zq") {
		t.Fatalf("redacted capture keeps a real name:\n%s", data)
	}

	listing, err := runOffline("replay", before)
	if err != nil || !strings.Contains(listing, "namespace-1") || !strings.Contains(listing, "pod-2") || strings.Contains(listing, "zq") {
		t.Fatalf("replay did not list the aliases: %v\n%s", err, listing)
	}
	replayed, err := runOffline("replay", before, "--pod", "namespace-1/pod-1")
	if err != nil || !strings.Contains(replayed, "Pod: namespace-1/pod-1") || !strings.Contains(replayed, "container-1") || !strings.Contains(replayed, "Captured history:") {
		t.Fatalf("replay by alias failed: %v\n%s", err, replayed)
	}
	_, err = runOffline("replay", before, "--pod", "zq-shop/zq-web-7f9-abc")
	if err == nil || !strings.Contains(err.Error(), "Redacted captures use capture-local aliases instead of cluster names; Pods in this file: namespace-1/pod-1, namespace-1/pod-2") {
		t.Fatalf("replay by real name error = %v", err)
	}

	compared, err := runOffline("compare", "--before", before, "--after", after, "--pod", "namespace-1/pod-2")
	if err != nil || !strings.Contains(compared, "Incident comparison: namespace-1/pod-2") || !strings.Contains(compared, "number aliases separately in each file") {
		t.Fatalf("compare by alias failed: %v\n%s", err, compared)
	}
	compared, err = runOffline("compare", "--before", before, "--after", after, "--workload", "namespace-1/Deployment/deployment-1")
	if err != nil || !strings.Contains(compared, "Workload incident comparison") {
		t.Fatalf("workload compare by alias failed: %v\n%s", err, compared)
	}
	_, err = runOffline("compare", "--before", before, "--after", after, "--workload", "zq-shop/Deployment/zq-web")
	if err == nil || !strings.Contains(err.Error(), "workloads in this file: namespace-1/Deployment/deployment-1") {
		t.Fatalf("workload compare by real name error = %v", err)
	}
}

func TestSinglePodRedactedCapturePrintsItsAlias(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pod.json")
	printed := captureAgainstCollector(t, "-n", "zq-shop", "--pod", "zq-web-7f9-def", "-o", path)
	if !strings.Contains(printed, "Replay or compare this Pod as --pod namespace-1/pod-1.") {
		t.Fatalf("single-Pod capture did not name its alias:\n%s", printed)
	}
	if replayed, err := runOffline("replay", path, "--pod", "namespace-1/pod-1"); err != nil {
		t.Fatalf("replay of single-Pod capture: %v\n%s", err, replayed)
	}
}

func TestSensitiveCaptureKeepsRealNames(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sensitive.json")
	printed := captureAgainstCollector(t, "-n", "zq-shop", "--include-sensitive", "-o", path)
	if !strings.Contains(printed, "redacted=false") || strings.Contains(printed, "alias") {
		t.Fatalf("sensitive capture output changed:\n%s", printed)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, kept := range []string{`"redacted": false`, "zq-web-7f9-abc", "zq-node-a", "zq-proxy", "zq-web", "zq-sandbox", "containerd://zq-web-7f9-abc-zq-app"} {
		if !strings.Contains(string(data), kept) {
			t.Fatalf("sensitive capture lost %q", kept)
		}
	}
	if replayed, err := runOffline("replay", path, "--pod", "zq-shop/zq-web-7f9-abc"); err != nil {
		t.Fatalf("sensitive replay by real name: %v\n%s", err, replayed)
	}
	if _, err := runOffline("replay", path, "--pod", "namespace-1/pod-1"); err == nil || err.Error() != "Pod namespace-1/pod-1 was not found in the incident bundle" {
		t.Fatalf("sensitive replay error changed: %v", err)
	}
}

// legacyRedactedIncident is the form written before issue 322: marked
// redacted, with UIDs removed but real names kept.
const legacyRedactedIncident = `{
  "schemaVersion": 2,
  "capturedAt": "%s",
  "toolVersion": "v1.0.4",
  "redacted": true,
  "pods": [{"namespace": "shop", "podName": "web-0", "podUID": "", "nodeName": "node-a", "capturedAt": "%s",
    "freshness": "fresh", "completeness": "complete",
    "containers": [{"namespace": "shop", "podName": "web-0", "podUID": "", "containerName": "app", "containerID": "", "nodeName": "node-a", "cgroupPath": "", "capturedAt": "%s",
      "freshness": "fresh", "completeness": "complete", "deltaWindowKnown": false,
      "context": {"qosClass": "Burstable", "podPhase": "Running", "ownerKind": "StatefulSet", "ownerName": "web", "workloadKind": "StatefulSet", "workloadName": "web"},
      "memory": {"Name": "shop/web-0/app", "TotalBytes": %d}}],
    "context": {"qosClass": "Burstable", "phase": "Running", "ownerKind": "StatefulSet", "ownerName": "web", "workloadKind": "StatefulSet", "workloadName": "web"},
    "memory": {"Name": "shop/web-0", "TotalBytes": %d}}],
  "nodes": [{"nodeName": "node-a", "capturedAt": "%s", "containerCount": 1, "stale": false, "freshness": "fresh", "completeness": "complete", "environment": {}}]
}`

func writeLegacyRedactedIncident(t *testing.T, path string, at time.Time, total uint64) {
	t.Helper()
	stamp := at.Format(time.RFC3339)
	body := fmt.Sprintf(legacyRedactedIncident, stamp, stamp, stamp, total, total, stamp)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLegacyRedactedIncidentWithRealNamesStillReplaysAndCompares(t *testing.T) {
	dir := t.TempDir()
	before, after := filepath.Join(dir, "before.json"), filepath.Join(dir, "after.json")
	now := time.Now().UTC().Truncate(time.Second)
	writeLegacyRedactedIncident(t, before, now, 100<<20)
	writeLegacyRedactedIncident(t, after, now.Add(time.Minute), 120<<20)
	replayed, err := runOffline("replay", before, "--pod", "shop/web-0")
	if err != nil || !strings.Contains(replayed, "Pod: shop/web-0") || !strings.Contains(replayed, "Redacted: true") {
		t.Fatalf("legacy redacted replay failed: %v\n%s", err, replayed)
	}
	compared, err := runOffline("compare", "--before", before, "--after", after, "--pod", "shop/web-0")
	if err != nil || !strings.Contains(compared, "Incident comparison: shop/web-0") {
		t.Fatalf("legacy redacted compare failed: %v\n%s", err, compared)
	}
	compared, err = runOffline("compare", "--before", before, "--after", after, "--workload", "shop/StatefulSet/web")
	if err != nil || !strings.Contains(compared, "Workload incident comparison") {
		t.Fatalf("legacy redacted workload compare failed: %v\n%s", err, compared)
	}
}
