package cli

import (
	"bytes"
	"io"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/client"
	"github.com/danushkastanley/kube-memlens/internal/collector"
	"github.com/danushkastanley/kube-memlens/internal/model"
)

// topCommandStore holds one node: a two-Pod StatefulSet and a bare Pod, both
// labelled by tier, and one cgroup the agent could not map to any Pod.
func topCommandStore(t *testing.T) *collector.Store {
	t.Helper()
	now := time.Now().UTC()
	container := func(namespace, pod, name, id string, labels map[string]string, workload string, memory model.MemoryBreakdown) api.ContainerSnapshot {
		snapshot := api.ContainerSnapshot{
			Namespace: namespace, PodName: pod, PodUID: namespace + "-" + pod, ContainerName: name, ContainerID: id,
			NodeName: "node-a", CapturedAt: now, Memory: memory,
			Context: api.ContainerContext{Labels: labels},
		}
		if workload != "" {
			snapshot.Context.WorkloadKind, snapshot.Context.WorkloadName = "StatefulSet", workload
		}
		return snapshot
	}
	web := map[string]string{"tier": "web"}
	batch := map[string]string{"tier": "batch"}
	containers := []api.ContainerSnapshot{
		container("shop", "web-0", "app", "id-web-0-app", web, "web", model.MemoryBreakdown{TotalBytes: 300 << 20, MaxKnown: true, MaxBytes: 512 << 20}),
		container("shop", "web-0", "proxy", "id-web-0-proxy", web, "web", model.MemoryBreakdown{TotalBytes: 40 << 20, MaxKnown: true, MaxUnlimited: true}),
		container("shop", "web-1", "app", "id-web-1-app", web, "web", model.MemoryBreakdown{TotalBytes: 280 << 20}),
		container("batch", "report", "worker", "id-report-worker", batch, "", model.MemoryBreakdown{TotalBytes: 96 << 20, MaxKnown: true, MaxBytes: 128 << 20}),
		{ContainerID: "id-unmapped", NodeName: "node-a", CapturedAt: now, Memory: model.MemoryBreakdown{TotalBytes: 7 << 20}},
	}
	store := collector.NewStore()
	if _, err := store.ReplaceNodeSnapshot(api.AgentSnapshot{NodeName: "node-a", CapturedAt: now, Containers: containers}); err != nil {
		t.Fatalf("ReplaceNodeSnapshot: %v", err)
	}
	return store
}

// runTop runs `top <args>` against the store through the collector's read
// handler and returns the table the user would see.
func runTop(t *testing.T, store *collector.Store, args ...string) string {
	t.Helper()
	server := httptest.NewServer(collector.NewReadHandlerWithOptions(store, collector.DefaultHandlerOptions(time.Minute)))
	defer server.Close()
	var output bytes.Buffer
	command := newTopCommand(func() client.Options {
		return client.Options{Mode: client.ConnectionModeHTTP, CollectorURL: server.URL}
	})
	command.SetOut(&output)
	command.SetErr(io.Discard)
	command.SetArgs(args)
	if err := command.Execute(); err != nil {
		t.Fatalf("top %s: %v", strings.Join(args, " "), err)
	}
	return output.String()
}

var tableHeaderCell = regexp.MustCompile(`\S+( \S+)*`)

// tableRows reads a tabwriter table by the header's column offsets, so empty
// cells and headers such as "LARGEST POD" keep their place.
func tableRows(t *testing.T, table string) []map[string]string {
	t.Helper()
	lines := strings.Split(strings.TrimRight(table, "\n"), "\n")
	header := lines[0]
	bounds := tableHeaderCell.FindAllStringIndex(header, -1)
	if len(bounds) == 0 {
		t.Fatalf("table has no header:\n%s", table)
	}
	rows := make([]map[string]string, 0, len(lines)-1)
	for _, line := range lines[1:] {
		row := map[string]string{}
		for index, bound := range bounds {
			start, end := bound[0], len(line)
			if index+1 < len(bounds) {
				end = min(bounds[index+1][0], len(line))
			}
			if start < len(line) {
				row[header[bound[0]:bound[1]]] = strings.TrimSpace(line[start:end])
			}
		}
		rows = append(rows, row)
	}
	return rows
}

func rowsByName(t *testing.T, rows []map[string]string, column string) map[string]map[string]string {
	t.Helper()
	byName := map[string]map[string]string{}
	for _, row := range rows {
		byName[row[column]] = row
	}
	return byName
}

func TestTopContainersListsOnlyContainersMappedToAPod(t *testing.T) {
	rows := tableRows(t, runTop(t, topCommandStore(t), "containers", "-A"))
	if len(rows) != 4 {
		t.Fatalf("top containers -A shows %d rows, want the 4 containers that belong to Pods: %v", len(rows), rows)
	}
	for _, row := range rows {
		if row["NAMESPACE"] == "" || row["POD"] == "" || row["CONTAINER"] == "" || row["TOTAL"] == "7Mi" {
			t.Errorf("top containers shows a cgroup that belongs to no Pod: %v", row)
		}
	}
}

func TestTopContainersShowsUnknownAndUnlimitedLimitsApart(t *testing.T) {
	rows := tableRows(t, runTop(t, topCommandStore(t), "containers", "-n", "shop"))
	want := map[string]string{"web-0/app": "512Mi", "web-0/proxy": "unlimited", "web-1/app": "unknown"}
	if len(rows) != len(want) {
		t.Fatalf("top containers -n shop shows %d rows, want %d: %v", len(rows), len(want), rows)
	}
	for _, row := range rows {
		name := row["POD"] + "/" + row["CONTAINER"]
		if row["LIMIT"] != want[name] {
			t.Errorf("container %s LIMIT = %q, want %q", name, row["LIMIT"], want[name])
		}
	}
}

func TestTopContainersLabelSelectorKeepsOnlyMatchingPods(t *testing.T) {
	rows := tableRows(t, runTop(t, topCommandStore(t), "containers", "-A", "-l", "tier=batch"))
	if len(rows) != 1 || rows[0]["POD"] != "report" || rows[0]["CONTAINER"] != "worker" {
		t.Fatalf("top containers -l tier=batch = %v, want only report/worker", rows)
	}
}

func TestTopWorkloadsLabelSelectorMatchesWorkloadsByTheirPods(t *testing.T) {
	store := topCommandStore(t)
	rows := tableRows(t, runTop(t, store, "workloads", "-A", "-l", "tier=web"))
	if len(rows) != 1 || rows[0]["KIND"] != "StatefulSet" || rows[0]["WORKLOAD"] != "web" {
		t.Fatalf("top workloads -l tier=web = %v, want only StatefulSet/web, whose Pods carry the label", rows)
	}
	if table := runTop(t, store, "workloads", "-A", "-l", "tier=none"); len(tableRows(t, table)) != 0 {
		t.Fatalf("top workloads -l tier=none shows workloads no Pod of which carries the label:\n%s", table)
	}
}

func TestTopWorkloadsRowCountsAndSumsItsPods(t *testing.T) {
	rows := rowsByName(t, tableRows(t, runTop(t, topCommandStore(t), "workloads", "-A")), "WORKLOAD")
	want := map[string][3]string{"web": {"2", "620Mi", "340Mi"}, "report": {"1", "96Mi", "96Mi"}}
	if len(rows) != len(want) {
		t.Fatalf("top workloads -A shows %v, want rows for %v", rows, want)
	}
	for name, cells := range want {
		row := rows[name]
		if got := [3]string{row["PODS"], row["TOTAL"], row["MAX POD"]}; got != cells {
			t.Errorf("workload %s PODS, TOTAL, MAX POD = %v, want %v", name, got, cells)
		}
	}
}

func TestTopNamespacesRowAgreesWithItsPods(t *testing.T) {
	rows := rowsByName(t, tableRows(t, runTop(t, topCommandStore(t), "ns", "-A")), "NAMESPACE")
	want := map[string][2]string{"shop": {"2", "620Mi"}, "batch": {"1", "96Mi"}}
	if len(rows) != len(want) {
		t.Fatalf("top ns -A shows %v, want rows for %v and none for the unmapped cgroup", rows, want)
	}
	for name, cells := range want {
		row := rows[name]
		if got := [2]string{row["PODS"], row["TOTAL"]}; got != cells {
			t.Errorf("namespace %s PODS, TOTAL = %v, want %v", name, got, cells)
		}
	}
}
