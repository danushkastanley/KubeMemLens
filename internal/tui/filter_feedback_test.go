package tui

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/model"
	"github.com/danushkastanley/kube-memlens/internal/observationview"
)

// feedbackPods returns one Pod per structured filter outcome: a recent OOM kill
// that only the Pod analysis sees, memory pressure, stale evidence and a calm
// baseline.
func feedbackPods(now time.Time) []api.PodSnapshot {
	calm := model.MemoryBreakdown{TotalBytes: 10 << 20, AnonBytes: 4 << 20, FileBytes: 2 << 20, EventDeltasKnown: true}
	oomKilled := api.PodContext{LastTerminationKnown: true, LastTerminationReason: "OOMKilled", LastTerminationExitCode: 137, LastTerminationFinishedAt: now.Add(-time.Minute)}
	pressured := calm
	pressured.PressureKnown, pressured.PSISomeAvg10 = true, 5
	return []api.PodSnapshot{
		{Namespace: "default", PodName: "restarted", NodeName: "node-a", CapturedAt: now, Context: oomKilled, Memory: calm},
		{Namespace: "default", PodName: "pressured", NodeName: "node-a", CapturedAt: now, Memory: pressured},
		{Namespace: "default", PodName: "lagging", NodeName: "node-a", CapturedAt: now.Add(-10 * time.Minute), Memory: calm},
		{Namespace: "default", PodName: "steady", NodeName: "node-a", CapturedAt: now, Memory: calm},
	}
}

func feedbackModel(t *testing.T, now time.Time) appModel {
	t.Helper()
	t.Setenv("NO_COLOR", "1")
	m := newModel(context.Background(), Options{AllNamespaces: true}, nil, "test")
	m.width, m.height = 130, 30
	m.resizeViewports()
	m.loading = false
	m.lastRefresh = now
	m.data.Pods = feedbackPods(now)
	m.data.Namespaces = []api.NamespaceSnapshot{{Namespace: "default", CapturedAt: now, PodCount: len(m.data.Pods)}}
	return m
}

func visiblePodNames(m appModel) []string {
	names := []string{}
	for _, pod := range m.visiblePods() {
		names = append(names, pod.PodName)
	}
	slices.Sort(names)
	return names
}

func TestTextFilterFindsPodFlaggedByRecentOOMKill(t *testing.T) {
	now := time.Now()
	got := FilterPodsAt(feedbackPods(now), "", true, "oom-risk", now, time.Minute)
	if len(got) != 1 || got[0].PodName != "restarted" {
		t.Fatalf("text filter oom-risk returned %d Pods %v; want only the Pod with a recent OOMKilled termination", len(got), got)
	}
}

func TestUnknownStructuredFilterValueIsReportedAndNotApplied(t *testing.T) {
	now := time.Now()
	all := []string{"lagging", "pressured", "restarted", "steady"}
	for _, check := range []struct {
		unknown, notice string
		valid           string
		validMatches    []string
	}{
		{"severity:warn", "Unknown filter severity:warn. Use info, medium, high or critical.", "sev:critical", []string{"restarted"}},
		{"pressure:high", "Unknown filter pressure:high. Use true or false.", "pressure:yes", []string{"pressured"}},
		{"state:old", "Unknown filter state:old. Use stale, fresh, incomplete or complete.", "state:stale", []string{"lagging"}},
		{"diag:leak", "Unknown filter diag:leak. Use part of cache-heavy,", "diagnosis:oom", []string{"restarted"}},
	} {
		m := feedbackModel(t, now)
		m.query = check.unknown
		if got := visiblePodNames(m); !slices.Equal(got, all) {
			t.Errorf("query %q filtered the table to %v; an unknown value must leave every Pod visible", check.unknown, got)
		}
		if frame := m.viewString(); !strings.Contains(frame, check.notice) {
			t.Errorf("query %q: frame lacks notice %q:\n%s", check.unknown, check.notice, frame)
		}
		m.query = check.valid
		if got := visiblePodNames(m); !slices.Equal(got, check.validMatches) {
			t.Errorf("query %q matched %v, want %v", check.valid, got, check.validMatches)
		}
		if frame := m.viewString(); strings.Contains(frame, "Unknown filter") {
			t.Errorf("query %q is valid but the frame reports an unknown filter:\n%s", check.valid, frame)
		}
	}
}

func TestFilterNoticeCountsFurtherUnknownTokensAndKeepsTypingVisible(t *testing.T) {
	m := feedbackModel(t, time.Now())
	m.searching = true
	m.query = "steady sev:warn pressure:high state:old"
	footer := m.renderFooter(200)
	notice := "Unknown filter sev:warn. Use info, medium, high or critical. 2 more filters are unknown."
	if !strings.HasPrefix(footer, notice) {
		t.Errorf("footer does not lead with the notice %q: %q", notice, footer)
	}
	for _, want := range []string{"search: steady sev:warn pressure:high state:old", "esc clear"} {
		if !strings.Contains(footer, want) {
			t.Errorf("footer lacks %q after the notice: %q", want, footer)
		}
	}
	if got := visiblePodNames(m); !slices.Equal(got, []string{"steady"}) {
		t.Errorf("text part of the query was not applied with unknown tokens present: %v", got)
	}
}

func TestPodRowShowsTheDiagnosisTheTextFilterMatches(t *testing.T) {
	m := feedbackModel(t, time.Now())
	m.query = "restarted"
	frame := m.viewString()
	if !strings.Contains(frame, "oom-risk") {
		t.Fatalf("Pod row for a recent OOM kill does not show oom-risk:\n%s", frame)
	}
}

func TestFilterNoticeFollowsTheKeysEachViewInterprets(t *testing.T) {
	now := time.Now()
	m := feedbackModel(t, now)
	m.data.Containers = []api.ContainerSnapshot{{Namespace: "default", PodName: "steady", ContainerName: "app", NodeName: "node-a", CapturedAt: now, Memory: m.data.Pods[3].Memory}}
	m.data.Workloads = []api.WorkloadSnapshot{{Namespace: "default", Kind: "Deployment", Name: "steady", CapturedAt: now, PodCount: 1, Memory: m.data.Pods[3].Memory}}
	m.data.ContainersLoaded = true
	for _, check := range []struct {
		view   viewMode
		query  string
		notice bool
	}{
		{viewContainers, "severity:warn", true},
		{viewWorkloads, "pressure:high", true},
		{viewContainers, "state:old", false},
		{viewNamespaces, "severity:warn", false},
		{viewNodes, "severity:warn", false},
	} {
		m.view, m.query = check.view, check.query
		frame := m.viewString()
		if got := strings.Contains(frame, "Unknown filter "+check.query); got != check.notice {
			t.Errorf("view %s query %q: notice shown = %t, want %t:\n%s", check.view, check.query, got, check.notice, frame)
		}
	}
}

// Restricted rows show labels such as "unreported" in their STATE column, and
// state: matches any of them. They must stay filters, never notices.
func TestRestrictedStateFilterMatchesEveryLabelTheRowShows(t *testing.T) {
	m, _ := restrictedModel(t)
	want := []string{}
	for _, row := range m.visibleObservationRows() {
		if observationview.State(row, time.Now()) == "unreported" {
			want = append(want, row.Name)
		}
	}
	if len(want) == 0 {
		t.Fatal("fixture has no restricted row in state unreported")
	}
	m.query = "state:unreported"
	got := []string{}
	for _, row := range m.visibleObservationRows() {
		got = append(got, row.Name)
	}
	if !slices.Equal(got, want) {
		t.Errorf("state:unreported matched %v, want the rows showing that state %v", got, want)
	}
	if footer := m.renderFooter(200); strings.Contains(footer, "Unknown filter") {
		t.Errorf("restricted view flagged a state label it applies: %q", footer)
	}
}

func TestRestrictedViewsKeepTheDeepEvidenceReasonForUnknownSeverity(t *testing.T) {
	m, _ := restrictedModel(t)
	m.query = "severity:warn"
	if footer := m.renderFooter(200); strings.Contains(footer, "Unknown filter") {
		t.Errorf("restricted view offered severity values it cannot evaluate: %q", footer)
	}
	if table := m.renderObservationTable(80); !strings.Contains(table, "require deep evidence") {
		t.Errorf("restricted table lost the deep-evidence reason for an unknown severity: %q", table)
	}
}

func TestFilterNoticeStaysOnOneLineInTheSmallestFrame(t *testing.T) {
	m := feedbackModel(t, time.Now())
	m.width, m.height = 40, 10
	m.resizeViewports()
	m.query = "diag:leak"
	lines := strings.Split(m.viewString(), "\n")
	if len(lines) > m.height || !strings.HasPrefix(lines[len(lines)-1], "Unknown filter diag:leak.") {
		t.Fatalf("40x10 frame has %d lines or lost the notice:\n%s", len(lines), strings.Join(lines, "\n"))
	}
}
