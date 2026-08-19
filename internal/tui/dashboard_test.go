package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestWidePodDashboardMatchesMemoryTroubleshootingHierarchy(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	m := loadedFixtureModel(t, 180, 50)
	frame := m.viewString()
	for _, want := range []string{
		"OBSERVED POD CHARGE", "NODE ALLOCATABLE", "CHARGE / ALLOCATABLE",
		"TOP NAMESPACE", "RISK PODS", "NAMESPACES", "PODS · RISK ORDER",
		"POD DETAILS", "NODE MEMORY CONTEXT", "CURRENT CGROUP SIGNALS",
		"A/F/S/O", "Pod cgroups only", "partial scope",
	} {
		if !strings.Contains(frame, want) {
			t.Fatalf("dashboard missing %q:\n%s", want, frame)
		}
	}
	for _, forbidden := range []string{"Cluster Memory Usage", "RECENT MEMORY EVENTS"} {
		if strings.Contains(frame, forbidden) {
			t.Fatalf("dashboard makes unsupported claim %q:\n%s", forbidden, frame)
		}
	}
	if height := lipgloss.Height(frame); height > 50 {
		t.Fatalf("dashboard height = %d, want <= 50:\n%s", height, frame)
	}
	for index, line := range strings.Split(frame, "\n") {
		if width := lipgloss.Width(line); width > 180 {
			t.Fatalf("line %d width = %d, want <= 180: %q", index+1, width, line)
		}
	}
}

func TestDashboardSignalPanelUsesSampledCgroupEvidence(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	m := loadedFixtureModel(t, 180, 50)
	frame := m.viewString()
	if !strings.Contains(frame, "high 1 PSI 2.0/0.0") {
		t.Fatalf("dashboard omitted current cgroup signal:\n%s", frame)
	}
	if strings.Contains(frame, "Evicted") {
		t.Fatalf("dashboard fabricated a Kubernetes eviction event:\n%s", frame)
	}
}
