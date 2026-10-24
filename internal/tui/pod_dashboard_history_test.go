package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	memmodel "github.com/danushkastanley/kube-memlens/internal/model"
)

func TestPodDashboardHistoryKeepsTimeGapsAndScale(t *testing.T) {
	now := time.Now().UTC()
	points := []api.MemoryHistoryPoint{{CapturedAt: now, TotalBytes: 50}, {CapturedAt: now.Add(time.Minute), TotalBytes: 50}}
	plot := strings.Join(podHistoryPlot(points, memmodel.MemoryBreakdown{MaxKnown: true, MaxBytes: 100}, 40), "\n")
	if !strings.Contains(plot, "0–100B") || strings.Count(plot, "•") != 2 || !strings.Contains(plot, strings.Repeat(" ", 28)) {
		t.Fatalf("history interpolated a gap or lost its zero baseline: %s", plot)
	}
}

func TestPodDashboardHistoryDoesNotReuseReplacedPod(t *testing.T) {
	m := loadedFixtureModel(t, 180, 50)
	pod := m.data.Pods[0]
	m.selectedHistory.series = []api.PodHistory{{PodUID: "old-instance", NodeName: pod.NodeName, Points: []api.MemoryHistoryPoint{{TotalBytes: 999}}}}
	if lines := strings.Join(m.podDashboardHistory(pod, 70), "\n"); !strings.Contains(lines, "No recent points") || strings.Contains(lines, "999") {
		t.Fatalf("history leaked across Pod identities: %s", lines)
	}
	m.selectedHistory.series[0].PodUID = pod.PodUID
	m.selectedHistory.err = errors.New("temporary network failure")
	if !strings.Contains(strings.Join(m.podDashboardHistory(pod, 70), "\n"), "last good") {
		t.Fatal("failed history refresh concealed last-good status")
	}
}

func TestPodDashboardReturnAfterResizeFitsMain(t *testing.T) {
	m := loadedFixtureModel(t, 180, 50)
	m = dashboardPress(m, "enter")
	m.width, m.height = 80, 24
	m.resizeViewports()
	m = dashboardPress(m, "esc")
	if m.currentViewport().capacity != m.layout().tableRows() {
		t.Fatal("returned viewport retained obsolete size")
	}
}
