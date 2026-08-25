package tui

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/model"
)

func TestLayoutBreakpoints(t *testing.T) {
	tests := []struct {
		name          string
		width, height int
		wantMode      layoutMode
		wantSplit     bool
		wantMinimumOK bool
	}{
		{name: "too small", width: 39, height: 9, wantMode: layoutCompact, wantMinimumOK: false},
		{name: "compact", width: 80, height: 24, wantMode: layoutCompact, wantMinimumOK: true},
		{name: "standard", width: 120, height: 30, wantMode: layoutStandard, wantMinimumOK: true},
		{name: "wide", width: 180, height: 50, wantMode: layoutWide, wantSplit: true, wantMinimumOK: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			plan := layoutFor(test.width, test.height, viewPods)
			if plan.mode != test.wantMode || plan.splitDetail != test.wantSplit || plan.minimumValid != test.wantMinimumOK {
				t.Fatalf("layout = %#v", plan)
			}
			if plan.tableWidth < 1 || plan.bodyRows < 5 {
				t.Fatalf("invalid dimensions: %#v", plan)
			}
		})
	}
}

func TestDetailViewDoesNotSplitAtWideWidth(t *testing.T) {
	plan := layoutFor(180, 50, viewDetail)
	if plan.splitDetail || plan.tableWidth != 180 {
		t.Fatalf("detail layout = %#v", plan)
	}
}

func TestWideLayoutRendersMasterDetailAndChangesFocus(t *testing.T) {
	m := newModel(context.Background(), Options{AllNamespaces: true}, nil, "test")
	m.view = viewPods
	m.loading = false
	m.width, m.height = 180, 50
	m.data.Namespaces = []api.NamespaceSnapshot{{Namespace: "default"}}
	m.data.Pods = []api.PodSnapshot{{
		Namespace: "default", PodName: "api", NodeName: "node-a",
		Memory: model.MemoryBreakdown{TotalBytes: 100, AnonBytes: 60, FileBytes: 30},
	}}
	m.resizeViewports()
	m.reconcileCurrentViewport("")

	frame := m.viewString()
	for _, want := range []string{"POD", "api", "│", "Likely explanation", "focus: table"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("wide frame missing %q:\n%s", want, frame)
		}
	}

	updated, _ := m.handleKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyTab}))
	m = updated.(appModel)
	if m.focus != focusDetail || !strings.Contains(m.viewString(), "focus: detail") {
		t.Fatalf("Tab did not move focus to detail")
	}
}

func TestResizeFromWideToCompactRestoresTableFocus(t *testing.T) {
	m := newModel(context.Background(), Options{}, nil, "test")
	m.view = viewPods
	m.width, m.height = 180, 50
	m.focus = focusDetail
	m.resizeViewports()

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(appModel)
	if m.focus != focusTable || m.layout().mode != layoutCompact {
		t.Fatalf("resized model focus=%v layout=%#v", m.focus, m.layout())
	}
}

func TestCompactHeaderKeepsActiveSortVisible(t *testing.T) {
	m := loadedFixtureModel(t, 80, 24)
	m.connectionDescription = "kube-proxy namespace/service-with-a-long-name:8080"
	m.sort = sortTotal
	if frame := m.viewString(); !strings.Contains(strings.Split(frame, "\n")[0], "sort: total desc") {
		t.Fatalf("compact header hid the active sort mode:\n%s", frame)
	}
}

func TestCompactHeaderKeepsMeaningfulStateVisible(t *testing.T) {
	m := loadedFixtureModel(t, 80, 24)
	m.connectionDescription = "kube-proxy namespace/service-with-a-long-name:8080"
	m.loading = true
	m.paused = true
	header := strings.Split(m.viewString(), "\n")[0]
	for _, want := range []string{"state: ready", "last update:"} {
		if !strings.Contains(header, want) {
			t.Fatalf("compact header hid %q: %q", want, header)
		}
	}
	if strings.Contains(header, "refreshing") {
		t.Fatalf("compact header exposed transient refresh state: %q", header)
	}
}

func TestHeaderSeparatesEvidenceAndRefreshState(t *testing.T) {
	m := loadedFixtureModel(t, 160, 35)
	m.paused = true
	header := m.renderHeader(160)
	for _, want := range []string{"state: ready", "refresh: paused"} {
		if !strings.Contains(header, want) {
			t.Fatalf("paused header hid %q: %q", want, header)
		}
	}

	m.paused = false
	header = m.renderHeader(160)
	for _, want := range []string{"state: ready", "refresh: automatic"} {
		if !strings.Contains(header, want) {
			t.Fatalf("automatic header hid %q: %q", want, header)
		}
	}
}
