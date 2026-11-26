package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/aggregate"
	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/client"
	"github.com/danushkastanley/kube-memlens/internal/collector"
	"github.com/danushkastanley/kube-memlens/internal/explain"
	"github.com/danushkastanley/kube-memlens/internal/model"
)

// memoryBudgetContainers is the kind harness fixture: a Pod-level limit of
// 256Mi, container app limited to 64Mi and container shared with no limit.
func memoryBudgetContainers(pod string, cgroup model.PodCgroupMemory, now time.Time) []api.ContainerSnapshot {
	app := closestContainerFixture(pod, "app", 30, 64, now)
	shared := closestContainerFixture(pod, "shared", 200, 0, now)
	shared.Memory.MaxUnlimited = true
	app.PodCgroup, shared.PodCgroup = cgroup, cgroup
	return []api.ContainerSnapshot{app, shared}
}

func memoryBudgetCgroup() model.PodCgroupMemory {
	return model.PodCgroupMemory{CurrentKnown: true, CurrentBytes: 240 << 20, MaxKnown: true, MaxBytes: 256 << 20, LocalEventsKnown: true}
}

// runTopAgainstPodCgroupCollector stores one schema 8 snapshot holding the
// memory-budget Pod and the same Pod shape without a Pod cgroup (legacy), then
// runs `top <resource>` through the collector's HTTP read path.
func runTopAgainstPodCgroupCollector(t *testing.T, args ...string) string {
	t.Helper()
	now := time.Now().UTC()
	containers := append(memoryBudgetContainers("memory-budget", memoryBudgetCgroup(), now), memoryBudgetContainers("legacy", model.PodCgroupMemory{}, now)...)
	store := collector.NewStore()
	if _, err := store.ReplaceNodeSnapshot(api.AgentSnapshot{SchemaVersion: api.PodCgroupSnapshotSchemaVersion, NodeName: "node-a", CapturedAt: now, Containers: containers}); err != nil {
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
	command.SetArgs(append(args[:1:1], append([]string{"-n", "shop"}, args[1:]...)...))
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	return output.String()
}

func TestTopPodsUsesThePodCgroupLimit(t *testing.T) {
	cells := map[string]string{}
	output := runTopAgainstPodCgroupCollector(t, "pods")
	for _, line := range strings.Split(strings.TrimSpace(output), "\n")[1:] {
		fields := strings.Fields(line)
		cells[fields[1]] = fields[4]
	}
	if cells["memory-budget"] != "256Mi" {
		t.Errorf("memory-budget LIMIT = %q, want the Pod cgroup's 256Mi:\n%s", cells["memory-budget"], output)
	}
	if cells["legacy"] != "unlimited" {
		t.Errorf("legacy LIMIT = %q, want unlimited from the container sum as before:\n%s", cells["legacy"], output)
	}
}

func TestTopPodsJSONNamesTheLimitSourceOnlyWhenThePodCgroupWasRead(t *testing.T) {
	var rows []map[string]any
	if err := json.Unmarshal([]byte(runTopAgainstPodCgroupCollector(t, "pods", "-o", "json")), &rows); err != nil {
		t.Fatal(err)
	}
	byName := map[string]map[string]any{}
	for _, row := range rows {
		byName[row["name"].(string)] = row
	}
	budget := byName["memory-budget"]
	if budget["limitState"] != "finite" || budget["limitBytes"] != float64(268435456) || budget["limitSource"] != "pod-cgroup" {
		t.Errorf("memory-budget row = %v, want limitState finite, limitBytes 268435456, limitSource pod-cgroup", budget)
	}
	legacy := byName["legacy"]
	if _, present := legacy["limitSource"]; present || legacy["limitState"] != "unlimited" {
		t.Errorf("legacy row = %v, want limitState unlimited and no limitSource", legacy)
	}
}

func TestTopContainersKeepsTheContainerLimit(t *testing.T) {
	output := runTopAgainstPodCgroupCollector(t, "containers")
	for _, line := range strings.Split(strings.TrimSpace(output), "\n")[1:] {
		fields := strings.Fields(line)
		if fields[1] == "memory-budget" && fields[2] == "shared" && fields[5] != "unlimited" {
			t.Fatalf("shared LIMIT = %q, want unlimited: a container keeps its own cgroup limit\n%s", fields[5], output)
		}
	}
}

func memoryBudgetPod(t *testing.T, cgroup model.PodCgroupMemory) api.PodSnapshot {
	t.Helper()
	pods := aggregate.Pods(memoryBudgetContainers("memory-budget", cgroup, time.Unix(1000, 0).UTC()))
	if len(pods) != 1 {
		t.Fatalf("fixture built %d Pods", len(pods))
	}
	return pods[0]
}

func TestExplainPodShowsThePodCgroup(t *testing.T) {
	cgroup := memoryBudgetCgroup()
	cgroup.LocalEventDeltasKnown, cgroup.LocalOOMKillEvents, cgroup.LocalOOMKillEventsDelta = true, 1, 1
	var output bytes.Buffer
	printPodExplanation(&output, memoryBudgetPod(t, cgroup), evidenceClock{at: time.Unix(1000, 0).UTC(), relation: "ago"})
	text := output.String()
	for _, want := range []string{
		"Hard limit:           256.00 MiB (94% used)\nLimit source:         Pod cgroup\n",
		"Pod cgroup charge:    240.00 MiB\n",
		"Pod cgroup limit:     256.00 MiB\n",
		"Pod cgroup events:    oom=0 oom_kill=1 max=0 in the sample window\n",
		"oom-risk\n",
		"Pod cgroup recorded 1 OOM kill(s) at its own limit in the sample window",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("explain pod output missing %q:\n%s", want, text)
		}
	}
}

func TestExplainPodMarksUnknownPodCgroupValues(t *testing.T) {
	var output bytes.Buffer
	printPodExplanation(&output, memoryBudgetPod(t, model.PodCgroupMemory{MaxKnown: true, MaxUnlimited: true}), evidenceClock{at: time.Unix(1000, 0).UTC(), relation: "ago"})
	text := output.String()
	for _, want := range []string{"Hard limit:           unlimited\nLimit source:         sum of container limits\n", "Pod cgroup charge:    unknown\n", "Pod cgroup limit:     unlimited\n", "Pod cgroup events:    unknown\n"} {
		if !strings.Contains(text, want) {
			t.Errorf("explain pod output missing %q:\n%s", want, text)
		}
	}
}

func TestExplainPodWithoutPodCgroupIsUnchanged(t *testing.T) {
	var output bytes.Buffer
	printPodExplanation(&output, memoryBudgetPod(t, model.PodCgroupMemory{}), evidenceClock{at: time.Unix(1000, 0).UTC(), relation: "ago"})
	text := output.String()
	if !strings.Contains(text, "Hard limit:           unlimited\n") {
		t.Errorf("Hard limit line changed:\n%s", text)
	}
	for _, absent := range []string{"Limit source", "Pod cgroup", "Zswap"} {
		if strings.Contains(text, absent) {
			t.Errorf("output without Pod cgroup evidence contains %q:\n%s", absent, text)
		}
	}
}

func TestExplainPodJSONCarriesThePodCgroupUnderSchema6(t *testing.T) {
	pod := memoryBudgetPod(t, memoryBudgetCgroup())
	pod.Memory.Zswap = model.Zswap{Known: true, PoolBytes: 4 << 20, SwappedBytes: 12 << 20}
	body, err := json.Marshal(podExplanationDocument(pod))
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(body, &document); err != nil {
		t.Fatal(err)
	}
	cgroup, _ := document["podCgroup"].(map[string]any)
	limit, _ := cgroup["limit"].(map[string]any)
	events, _ := cgroup["recentEvents"].(map[string]any)
	if document["schemaVersion"] != float64(6) || cgroup["effectiveLimitSource"] != "pod-cgroup" || limit["bytes"] != float64(256<<20) || events["known"] != false {
		t.Fatalf("explanation = %s", body)
	}
	zswap, _ := document["memory"].(map[string]any)["zswap"].(map[string]any)
	if zswap["poolBytes"] != float64(4<<20) || zswap["swappedBytes"] != float64(12<<20) {
		t.Fatalf("memory.zswap = %v", zswap)
	}
}

func TestExplainPodJSONWithoutNewEvidenceStaysSchema3(t *testing.T) {
	body, err := json.Marshal(podExplanationDocument(memoryBudgetPod(t, model.PodCgroupMemory{})))
	if err != nil {
		t.Fatal(err)
	}
	for _, absent := range []string{`"podCgroup"`, `"zswap"`, `"oomGroupKill"`} {
		if bytes.Contains(body, []byte(absent)) {
			t.Fatalf("explanation without schema 8 evidence carries %s: %s", absent, body)
		}
	}
	if !bytes.Contains(body, []byte(`"schemaVersion":3`)) {
		t.Fatalf("explanation without schema 8 evidence is not schema 3: %s", body)
	}
}

func TestExplainShowsZswapOnlyWhenKnown(t *testing.T) {
	memory := model.MemoryBreakdown{TotalBytes: 64 << 20, Zswap: model.Zswap{Known: true}}
	var known, unknown bytes.Buffer
	printMemoryExplanation(&known, "Sample: zswap", memory, explain.Analyze(memory))
	if !strings.Contains(known.String(), "Zswap:                0 B pool holding 0 B swapped out\n") {
		t.Fatalf("a known zero zswap is not shown:\n%s", known.String())
	}
	memory.Zswap = model.Zswap{}
	printMemoryExplanation(&unknown, "Sample: zswap", memory, explain.Analyze(memory))
	if strings.Contains(unknown.String(), "Zswap") {
		t.Fatalf("an unreported zswap is shown:\n%s", unknown.String())
	}
}
