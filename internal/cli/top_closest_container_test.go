package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/client"
	"github.com/danushkastanley/kube-memlens/internal/collector"
	"github.com/danushkastanley/kube-memlens/internal/model"
)

func closestContainerFixture(pod, name string, usedMiB, limitMiB uint64, capturedAt time.Time) api.ContainerSnapshot {
	return api.ContainerSnapshot{
		Namespace: "shop", PodName: pod, PodUID: pod + "-uid", ContainerName: name, ContainerID: pod + "-" + name, NodeName: "node-a", CapturedAt: capturedAt,
		Memory: model.MemoryBreakdown{TotalBytes: usedMiB << 20, AnonBytes: usedMiB << 20, MaxKnown: true, MaxBytes: limitMiB << 20},
	}
}

// runTopPodsAgainstCollector drives `top pods` end to end: agent snapshot,
// collector aggregation, HTTP read and rendering.
func runTopPodsAgainstCollector(t *testing.T, args ...string) string {
	t.Helper()
	now := time.Now().UTC()
	store := collector.NewStore()
	containers := []api.ContainerSnapshot{
		// checkout is at 18% of its summed limits while app is at 99% of its own.
		closestContainerFixture("checkout", "app", 99, 100, now),
		closestContainerFixture("checkout", "cache", 100, 1000, now),
		// single has one container, so its Pod figure already says everything.
		closestContainerFixture("single", "app", 99, 100, now),
	}
	if _, err := store.ReplaceNodeSnapshot(api.AgentSnapshot{NodeName: "node-a", CapturedAt: now, Containers: containers}); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(collector.NewReadHandlerWithOptions(store, collector.DefaultHandlerOptions(time.Minute)))
	defer server.Close()

	var output bytes.Buffer
	command := newTopCommand(func() client.Options {
		return client.Options{Mode: client.ConnectionModeHTTP, CollectorURL: server.URL}
	})
	command.SetOut(&output)
	command.SetErr(io.Discard)
	command.SetArgs(append([]string{"pods", "-n", "shop"}, args...))
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	return output.String()
}

func TestTopPodsTableNamesTheContainerClosestToItsLimit(t *testing.T) {
	output := runTopPodsAgainstCollector(t)
	cells := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(output), "\n")[1:] {
		fields := strings.Fields(line)
		// NAMESPACE POD NODE TOTAL LIMIT...; the LIMIT cell may hold a space.
		cells[fields[1]] = strings.Join(fields[4:], " ")
	}
	if got := cells["checkout"]; !strings.HasPrefix(got, "1.07Gi (app 99%) ") {
		t.Errorf("checkout LIMIT cell starts %q, want the Pod limit followed by (app 99%%):\n%s", got, output)
	}
	if got := cells["single"]; !strings.HasPrefix(got, "100Mi ") || strings.Contains(got, "(") {
		t.Errorf("single-container Pod LIMIT cell = %q, want the Pod limit alone:\n%s", got, output)
	}
}

func TestTopPodsJSONCarriesTheClosestContainerOnlyWhenItAddsSomething(t *testing.T) {
	var rows []map[string]any
	if err := json.Unmarshal([]byte(runTopPodsAgainstCollector(t, "-o", "json")), &rows); err != nil {
		t.Fatal(err)
	}
	byName := map[string]map[string]any{}
	for _, row := range rows {
		byName[row["name"].(string)] = row
	}
	checkout := byName["checkout"]
	if checkout["limitClosestContainer"] != "app" || checkout["limitClosestContainerPercent"] != float64(99) {
		t.Errorf("checkout row = %v, want limitClosestContainer app at 99", checkout)
	}
	if checkout["limitState"] != "finite" || checkout["limitBytes"] != float64(1100<<20) {
		t.Errorf("checkout row changed its existing limit fields: %v", checkout)
	}
	single := byName["single"]
	for _, field := range []string{"limitClosestContainer", "limitClosestContainerPercent"} {
		if _, present := single[field]; present {
			t.Errorf("single-container row carries %s: %v", field, single)
		}
	}
}

func TestIncidentPodTableNamesTheContainerClosestToItsLimit(t *testing.T) {
	now := time.Now().UTC()
	app := closestContainerFixture("checkout", "app", 99, 100, now)
	sidecar := closestContainerFixture("checkout", "sidecar", 400, 0, now)
	sidecar.Memory.MaxUnlimited = true
	pod := api.PodSnapshot{
		Namespace: "shop", PodName: "checkout", NodeName: "node-a", CapturedAt: now,
		Containers: []api.ContainerSnapshot{app, sidecar},
		Memory:     model.MemoryBreakdown{TotalBytes: 499 << 20, MaxKnown: true, MaxUnlimited: true},
	}
	var output bytes.Buffer
	printPodsTable(&output, []api.PodSnapshot{pod}, now)
	if !strings.Contains(output.String(), "unlimited (app 99%)") {
		t.Fatalf("incident Pod table hides the container closest to its limit:\n%s", output.String())
	}
}
