package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/lipgloss"
	"github.com/danushkastanley/kube-memlens/internal/api"
	memmodel "github.com/danushkastanley/kube-memlens/internal/model"
)

func dashboardPress(m appModel, key string) appModel {
	updated, _ := m.handleKey(keyMessage(key))
	return updated.(appModel)
}

func TestPodDashboardEnterEscPreservesParentFrame(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	m := loadedFixtureModel(t, 180, 50)
	m.query = "api"
	m.sort = sortName
	m.currentViewport().move(1)
	before, key, vp := m.viewString(), m.selectedEntityKey(), *m.currentViewport()
	m = dashboardPress(m, "enter")
	if !m.isPodDashboard() || entityKey(m.detail) != key {
		t.Fatalf("Enter did not inspect selected pod: %+v", m.detail)
	}
	m = dashboardPress(m, "esc")
	if m.query != "api" || m.selectedEntityKey() != key || *m.currentViewport() != vp {
		t.Fatalf("return lost selection/filter/viewport: %+v", m)
	}
	if got := m.viewString(); got != before {
		t.Fatalf("main dashboard changed after drill/back:\nbefore:\n%s\nafter:\n%s", before, got)
	}
}

func TestPodDashboardReturnFindsIdentityAfterReorder(t *testing.T) {
	m := loadedFixtureModel(t, 180, 50)
	key := m.selectedEntityKey()
	m = dashboardPress(m, "enter")
	for i := range m.data.Pods {
		if podKey(m.data.Pods[i].Namespace, m.data.Pods[i].PodName) != key {
			m.data.Pods[i].Memory = memmodel.MemoryBreakdown{TotalBytes: 9 << 30, OOMKillEventsDelta: 5, EventDeltasKnown: true}
		}
	}
	m = dashboardPress(m, "esc")
	if m.selectedEntityKey() != key {
		t.Fatalf("selection moved to a different pod: %s", m.selectedEntityKey())
	}
}

func TestPodDashboardRemovedPodCanReturn(t *testing.T) {
	m := loadedFixtureModel(t, 180, 50)
	m = dashboardPress(m, "enter")
	m.data.Pods = m.data.Pods[:0]
	if !strings.Contains(m.renderPodDashboard(), "no longer present") {
		t.Fatal("missing pod presented as current")
	}
	m = dashboardPress(m, "esc")
	if m.view != viewPods {
		t.Fatal("cannot leave removed Pod")
	}
}

func TestPodDashboardLayoutsFitAndRetainEvidence(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	for _, size := range [][2]int{{40, 10}, {80, 24}, {120, 30}, {150, 40}, {180, 50}, {240, 65}} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			m := loadedFixtureModel(t, size[0], size[1])
			m = dashboardPress(m, "enter")
			for panel := podComposition; panel < podPanelCount; panel++ {
				m.podDashboard.panel = panel
				frame := m.viewString()
				if lipgloss.Height(frame) > size[1] {
					t.Fatalf("height %d exceeds %d:\n%s", lipgloss.Height(frame), size[1], frame)
				}
				for _, line := range strings.Split(frame, "\n") {
					if lipgloss.Width(line) > size[0] {
						t.Fatalf("width exceeds %d: %q", size[0], line)
					}
				}
			}
			if m.podDashboardWide() {
				for _, title := range podPanelTitles {
					if !strings.Contains(m.viewString(), title) {
						t.Fatalf("missing %s", title)
					}
				}
			}
		})
	}
}

func TestPodDashboardNestedViewsReturnToPod(t *testing.T) {
	for _, target := range []string{"raw", "container", "help"} {
		t.Run(target, func(t *testing.T) {
			m := loadedFixtureModel(t, 180, 50)
			m = dashboardPress(m, "enter")
			ref := m.detail
			switch target {
			case "raw":
				m = dashboardPress(m, "e")
			case "container":
				m.podDashboard.panel = podContainers
				m = dashboardPress(m, "enter")
			case "help":
				m = dashboardPress(m, "?")
			}
			m = dashboardPress(m, "esc")
			if !m.isPodDashboard() || m.detail != ref || m.help {
				t.Fatal("nested view did not restore Pod dashboard")
			}
			m = dashboardPress(m, "esc")
			if m.view != viewPods {
				t.Fatal("second Esc did not restore main dashboard")
			}
		})
	}
}

func TestPodDashboardEvidenceStatesAndLimits(t *testing.T) {
	m := loadedFixtureModel(t, 180, 50)
	pod := m.data.Pods[0]
	pod.Freshness = api.EvidenceFreshnessFresh
	pod.Completeness = api.EvidencePartial
	pod.CapturedAt = time.Now().Add(-time.Hour)
	if label := m.podEvidenceLabel(pod); !strings.Contains(label, "stale / partial") {
		t.Fatalf("expired evidence presented as current: %s", label)
	}
	m.statusErr = errors.New("connection failed")
	if !strings.Contains(m.podEvidenceLabel(pod), "last known") {
		t.Fatal("connection failure concealed")
	}
	pod.Memory = memmodel.MemoryBreakdown{}
	if podLimit(pod) != "unknown" || podHeadroom(pod) != "not available" {
		t.Fatal("unknown limit presented as zero")
	}
	pod.Memory.MaxKnown, pod.Memory.MaxUnlimited = true, true
	if podLimit(pod) != "unlimited" || podHeadroom(pod) != "not available" {
		t.Fatal("unlimited limit presented as zero")
	}
	pod.Memory.MaxUnlimited, pod.Memory.MaxBytes, pod.Memory.TotalBytes = false, 10, 11
	if !strings.Contains(podHeadroom(pod), "Exceeded") {
		t.Fatal("over-limit headroom underflow")
	}
	pod.Containers = nil
	if signals := strings.Join(podDashboardSignals(pod), "\n"); !strings.Contains(signals, "unavailable") || strings.Contains(signals, "OOM 0") {
		t.Fatalf("missing counter baseline presented as zero: %s", signals)
	}
}

func TestPodDashboardTabAndReadOnlyAction(t *testing.T) {
	m := loadedFixtureModel(t, 180, 50)
	m = dashboardPress(m, "enter")
	for range 3 {
		updated, _ := m.handleKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyTab}))
		m = updated.(appModel)
	}
	if m.podDashboard.panel != podChecks {
		t.Fatal("Tab does not move to checks")
	}
	updated, command := m.handleKey(keyMessage("enter"))
	m = updated.(appModel)
	if command == nil {
		t.Fatal("recommendation action did not dispatch")
	}
	m = applyCommand(t, m, command)
	if m.action.mode != actionResultMode || !strings.Contains(strings.Join(m.action.result.lines, "\n"), "Automatic mutation: disabled") {
		t.Fatal("read-only recommendation preview unavailable")
	}
}

func TestPodDashboardFocusIsVisibleWithoutColour(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	m := loadedFixtureModel(t, 180, 50)
	m = dashboardPress(m, "enter")
	if !strings.Contains(m.viewString(), "> WHERE MEMORY GOES") {
		t.Fatal("focused panel lacks a text marker")
	}
	updated, _ := m.handleKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyTab}))
	m = updated.(appModel)
	if !strings.Contains(m.viewString(), "> POD CONTEXT") || strings.Contains(m.viewString(), "> WHERE MEMORY GOES") {
		t.Fatal("focus marker did not move with keyboard focus")
	}
}
