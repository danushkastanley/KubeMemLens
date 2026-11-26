package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/rivo/uniseg"

	"github.com/danushkastanley/kube-memlens/internal/aggregate"
	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/model"
)

func limitCellContainer(pod, name string, usedMiB, limitMiB uint64) api.ContainerSnapshot {
	return api.ContainerSnapshot{
		Namespace: "shop", PodName: pod, PodUID: pod + "-uid", ContainerName: name, ContainerID: pod + "-" + name, NodeName: "node-a", CapturedAt: time.Now().UTC(),
		Memory: model.MemoryBreakdown{TotalBytes: usedMiB << 20, AnonBytes: usedMiB << 20, MaxKnown: true, MaxBytes: limitMiB << 20},
	}
}

// limitCellPods holds a Pod at 18% of its summed limits whose app container is
// at 99% of its own, an unlimited Pod whose only limited container has a long
// name, and a single-container Pod that has nothing extra to show.
func limitCellPods(t *testing.T) []api.PodSnapshot {
	t.Helper()
	proxy := limitCellContainer("payments", "istio-proxy", 300, 0)
	proxy.Memory.MaxUnlimited = true
	// single is mostly file cache, so its DIAGNOSIS cell reads cache-heavy.
	single := limitCellContainer("single", "app", 50, 100)
	single.Memory.AnonBytes, single.Memory.FileBytes = 10<<20, 40<<20
	pods := aggregate.Pods([]api.ContainerSnapshot{
		limitCellContainer("checkout", "app", 99, 100),
		limitCellContainer("checkout", "cache", 100, 1000),
		limitCellContainer("payments", "payments-worker", 99, 100),
		proxy,
		single,
	})
	if len(pods) != 3 {
		t.Fatalf("fixture built %d Pods, want 3", len(pods))
	}
	return pods
}

func limitCellPod(t *testing.T, name string) api.PodSnapshot {
	t.Helper()
	for _, pod := range limitCellPods(t) {
		if pod.PodName == name {
			return pod
		}
	}
	t.Fatalf("fixture has no Pod %s", name)
	return api.PodSnapshot{}
}

// However narrow the cell, the container's percentage is the part that
// survives: the name is shortened first and the bar goes next.
func TestPodLimitCellKeepsTheContainerPercentage(t *testing.T) {
	for _, name := range []string{"checkout", "payments"} {
		pod := limitCellPod(t, name)
		smallest := uniseg.StringWidth(podLimitCell(pod, 0, false))
		for width := smallest; width <= 48; width++ {
			cell := podLimitCell(pod, width, false)
			if uniseg.StringWidth(cell) > width || !strings.HasSuffix(cell, " 99%)") {
				t.Fatalf("%s at width %d: cell %q does not fit or lost the container percentage", name, width, cell)
			}
		}
	}

	checkout := limitCellPod(t, "checkout")
	if got := podLimitCell(checkout, 22, false); got != "█░░░░░░░ 18% (app 99%)" {
		t.Errorf("checkout at width 22 = %q, want the bar kept because the full name fits", got)
	}
	if got := podLimitCell(checkout, 19, false); got != "18% (app 99%)" {
		t.Errorf("checkout at width 19 = %q, want the bar dropped rather than the name", got)
	}
	payments := limitCellPod(t, "payments")
	if got := podLimitCell(payments, 19, false); got != "unlimited (pa… 99%)" {
		t.Errorf("payments at width 19 = %q, want a shortened name and the percentage", got)
	}
	if got := podLimitCell(limitCellPod(t, "single"), 48, false); strings.Contains(got, "(") {
		t.Errorf("single-container Pod cell = %q, want the Pod figure alone", got)
	}
}

func TestPodTableRowsShowTheContainerClosestToItsLimit(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	sizes := []struct {
		name          string
		width, height int
	}{
		{"compact 80 columns", 80, 24},
		{"standard table", 120, 30},
		{"wide dashboard", 180, 50},
	}
	for _, size := range sizes {
		t.Run(size.name, func(t *testing.T) {
			m := newModel(context.Background(), Options{AllNamespaces: true}, nil, "test")
			m.view, m.loading = viewPods, false
			m.width, m.height = size.width, size.height
			m.data.Namespaces = []api.NamespaceSnapshot{{Namespace: "shop"}}
			m.data.Pods = limitCellPods(t)
			m.resizeViewports()
			m.reconcileCurrentViewport("")
			frame := m.viewString()
			assertFrameBounds(t, frame, size.width, size.height)

			// The cells must survive tableRow, which cuts a cell wider than its column.
			want := map[string]string{"checkout": "18% (app 99%)", "payments": "unlimited (p"}
			for pod, cell := range want {
				line := frameLineWith(frame, pod+" ")
				if !strings.Contains(line, cell) || !strings.Contains(line, " 99%)") {
					t.Errorf("row for %s = %q, want %q and the container percentage on the same line", pod, line, cell)
				}
			}
			if line := frameLineWith(frame, "single "); line == "" || strings.Contains(line, "%)") {
				t.Errorf("single-container row = %q, want the Pod figure alone", line)
			}
		})
	}
}

// The wider LIMIT cell must not take room from the other Pod columns. At 120
// columns main shows DIAGNOSIS from display column 109, cut to "cache-heav…".
func TestPodLimitCellLeavesTheDiagnosisColumnAsOnMain(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	m := newModel(context.Background(), Options{AllNamespaces: true}, nil, "test")
	m.view, m.loading = viewPods, false
	m.width, m.height = 120, 30
	m.data.Namespaces = []api.NamespaceSnapshot{{Namespace: "shop"}}
	m.data.Pods = limitCellPods(t)
	m.resizeViewports()
	m.reconcileCurrentViewport("")
	line := frameLineWith(m.viewString(), "single ")
	index := strings.Index(line, "cache-heav…")
	if index < 0 || uniseg.StringWidth(line[:index]) != 109 {
		t.Fatalf("DIAGNOSIS for the cache-heavy Pod moved or shrank: %q", line)
	}
}

// main's dashboard Pod columns other than the name total 65; more would make
// rows wrap at widths where they did not wrap before.
func TestDashboardPodColumnsKeepMainsFixedWidth(t *testing.T) {
	for width := 150; width <= 180; width++ {
		total := 0
		for _, cellWidth := range dashboardPodWidths(width)[1:] {
			total += cellWidth
		}
		if total != 65 {
			t.Fatalf("width %d: fixed dashboard Pod columns total %d, want main's 65", width, total)
		}
	}
}

func TestPodDetailNamesTheContainerClosestToItsLimit(t *testing.T) {
	m := newModel(context.Background(), Options{AllNamespaces: true}, nil, "test")
	checkout, single := limitCellPod(t, "checkout"), limitCellPod(t, "single")
	const want = "app at 99% of its limit"
	for name, lines := range map[string][]string{"detail": m.podDetailLines(checkout, 100, "History"), "inline": compactPodLines(checkout)} {
		if !strings.Contains(strings.Join(lines, "\n"), "Closest container:") || !strings.Contains(strings.Join(lines, "\n"), want) {
			t.Errorf("%s pane omits %q:\n%s", name, want, strings.Join(lines, "\n"))
		}
	}
	for name, lines := range map[string][]string{"detail": m.podDetailLines(single, 100, "History"), "inline": compactPodLines(single)} {
		if strings.Contains(strings.Join(lines, "\n"), "Closest container") {
			t.Errorf("%s pane names a closest container for a single-container Pod", name)
		}
	}
}

func frameLineWith(frame, text string) string {
	for _, line := range strings.Split(frame, "\n") {
		if strings.Contains(line, text) {
			return line
		}
	}
	return ""
}
