package tui

import (
	"context"
	"strings"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/aggregate"
	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/model"
)

// podCgroupPods holds the kind harness's memory-budget Pod (Pod cgroup limit
// 256Mi, container app limited to 64Mi, container shared unlimited) and the
// same shape without a Pod cgroup, as an older agent reports it.
func podCgroupPods(t *testing.T, cgroup model.PodCgroupMemory) []api.PodSnapshot {
	t.Helper()
	var containers []api.ContainerSnapshot
	for _, pod := range []string{"memory-budget", "legacy"} {
		app, shared := limitCellContainer(pod, "app", 30, 64), limitCellContainer(pod, "shared", 200, 0)
		shared.Memory.MaxUnlimited = true
		if pod == "memory-budget" {
			app.PodCgroup, shared.PodCgroup = cgroup, cgroup
		}
		containers = append(containers, app, shared)
	}
	pods := aggregate.Pods(containers)
	if len(pods) != 2 {
		t.Fatalf("fixture built %d Pods, want 2", len(pods))
	}
	if pods[0].PodName != "memory-budget" {
		pods[0], pods[1] = pods[1], pods[0]
	}
	return pods
}

func budgetCgroup() model.PodCgroupMemory {
	return model.PodCgroupMemory{CurrentKnown: true, CurrentBytes: 240 << 20, MaxKnown: true, MaxBytes: 256 << 20, LocalEventsKnown: true, LocalEventDeltasKnown: true}
}

func TestPodRowsMeasureAgainstThePodCgroupLimit(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	for _, size := range []struct{ width, height int }{{80, 24}, {120, 30}, {180, 50}} {
		m := newModel(context.Background(), Options{AllNamespaces: true}, nil, "test")
		m.view, m.loading = viewPods, false
		m.width, m.height = size.width, size.height
		m.data.Namespaces = []api.NamespaceSnapshot{{Namespace: "shop"}}
		m.data.Pods = podCgroupPods(t, budgetCgroup())
		m.resizeViewports()
		m.reconcileCurrentViewport("")
		frame := m.viewString()
		assertFrameBounds(t, frame, size.width, size.height)
		if line := frameLineWith(frame, "memory-"); !strings.Contains(line, "94%") {
			t.Errorf("width %d: memory-budget row = %q, want 94%% of its Pod cgroup limit", size.width, line)
		}
		if line := frameLineWith(frame, "legacy "); !strings.Contains(line, "unlimited") {
			t.Errorf("width %d: legacy row = %q, want unlimited from the container sum", size.width, line)
		}
	}
}

func TestPodDetailNamesTheLimitSource(t *testing.T) {
	m := newModel(context.Background(), Options{AllNamespaces: true}, nil, "test")
	pods := podCgroupPods(t, budgetCgroup())
	budget, legacy := pods[0], pods[1]
	for name, lines := range map[string][]string{"detail": m.podDetailLines(budget, 100, "History"), "inline": compactPodLines(budget)} {
		text := strings.Join(lines, "\n")
		if !strings.Contains(text, "94% (Pod cgroup)") {
			t.Errorf("%s pane does not name the Pod cgroup on the Limit line:\n%s", name, text)
		}
	}
	detail := strings.Join(m.podDetailLines(budget, 100, "History"), "\n")
	for _, want := range []string{"Pod cgroup charge:    240Mi", "Pod cgroup limit:     256Mi", "Pod cgroup events:    oom 0 · oom_kill 0 · max 0 (recent)", "Hard limit:           256.00 MiB (94% used)"} {
		if !strings.Contains(detail, want) {
			t.Errorf("detail pane missing %q:\n%s", want, detail)
		}
	}
	for name, lines := range map[string][]string{"detail": m.podDetailLines(legacy, 100, "History"), "inline": compactPodLines(legacy)} {
		text := strings.Join(lines, "\n")
		if strings.Contains(text, "Pod cgroup") || strings.Contains(text, "sum of container limits") {
			t.Errorf("%s pane for a Pod without a Pod cgroup changed:\n%s", name, text)
		}
	}
}

func TestPodDetailShowsGroupKillsAndZswapOnlyWhenReported(t *testing.T) {
	m := newModel(context.Background(), Options{AllNamespaces: true}, nil, "test")
	pod := podCgroupPods(t, budgetCgroup())[0]
	pod.Memory.OOMGroupKill = model.OOMGroupKill{Known: true, Events: 2, DeltasKnown: true, EventsDelta: 1}
	pod.Memory.Zswap = model.Zswap{Known: true, PoolBytes: 4 << 20, SwappedBytes: 12 << 20}
	text := strings.Join(m.podDetailLines(pod, 100, "History"), "\n")
	for _, want := range []string{"Group kill events:    2", "Zswap:                4.00 MiB pool holding 12.00 MiB swapped out"} {
		if !strings.Contains(text, want) {
			t.Errorf("detail pane missing %q:\n%s", want, text)
		}
	}
	pod.Memory.OOMGroupKill, pod.Memory.Zswap = model.OOMGroupKill{}, model.Zswap{}
	text = strings.Join(m.podDetailLines(pod, 100, "History"), "\n")
	for _, absent := range []string{"Group kill", "Zswap"} {
		if strings.Contains(text, absent) {
			t.Errorf("detail pane shows an unreported %s:\n%s", absent, text)
		}
	}
}

func TestRecentPodCgroupOOMKillOrdersAndLabelsThePod(t *testing.T) {
	cgroup := budgetCgroup()
	cgroup.CurrentBytes = 100 << 20
	cgroup.LocalOOMKillEvents, cgroup.LocalOOMKillEventsDelta = 1, 1
	pod := podCgroupPods(t, cgroup)[0]
	risk := podRisk(pod, pod.CapturedAt, 0)
	if risk.label != "CRIT" || !strings.Contains(riskReasonsText(risk), "1 recent Pod cgroup OOM kill event(s)") {
		t.Fatalf("risk = %+v, want CRIT naming the Pod cgroup OOM kill", risk)
	}
	if got := podIncidentLabel(pod); got != "Pod OOM 0/1" {
		t.Fatalf("signal = %q, want Pod OOM 0/1", got)
	}
}
