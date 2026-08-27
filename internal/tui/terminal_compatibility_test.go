package tui

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/lipgloss"
)

func TestMinimumSizeStateIsBoundedAndRecovers(t *testing.T) {
	m := loadedFixtureModel(t, 80, 24)

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 39, Height: 9})
	m = updated.(appModel)
	frame := m.viewString()
	for _, want := range []string{"Terminal too small: 39x9.", "Resize to at least 40x10", "Press q"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("minimum-size frame missing %q:\n%s", want, frame)
		}
	}
	assertFrameBounds(t, frame, 39, 9)

	updated, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(appModel)
	frame = m.viewString()
	if strings.Contains(frame, "Terminal too small") || !strings.Contains(frame, "A/F/S/O") {
		t.Fatalf("supported layout did not recover:\n%s", frame)
	}
	assertFrameBounds(t, frame, 80, 24)
}

func TestResizeStormKeepsAValidViewportAndFocus(t *testing.T) {
	m := loadedFixtureModel(t, 180, 50)
	m.focus = focusDetail

	for _, size := range [][2]int{{1, 1}, {39, 9}, {40, 10}, {79, 23}, {80, 24}, {120, 30}, {180, 50}, {60, 12}, {180, 50}} {
		updated, _ := m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		m = updated.(appModel)
		frame := m.viewString()
		assertFrameBounds(t, frame, size[0], size[1])
		if !m.layout().splitDetail && m.focus != focusTable {
			t.Fatalf("size %dx%d retained hidden detail focus", size[0], size[1])
		}
		start, end := m.activeViewport().visibleRange()
		if start < 0 || end < start || end > m.activeViewport().count {
			t.Fatalf("size %dx%d has invalid viewport %d:%d/%d", size[0], size[1], start, end, m.activeViewport().count)
		}
	}
}

func TestSupportedSizesKeepNavigationPagingFocusAndHelpUsable(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {120, 30}, {180, 50}} {
		m := loadedFixtureModel(t, size[0], size[1])
		for _, key := range []string{"N", "n", "w", "p", "c", "pgdown", "pgup", "G", "g", "?"} {
			updated, _ := m.handleKey(keyMessage(key))
			m = updated.(appModel)
			assertFrameBounds(t, m.viewString(), size[0], size[1])
		}
		if !m.help || !strings.Contains(m.viewString(), "Keybindings") {
			t.Fatalf("help did not open at %dx%d", size[0], size[1])
		}
		if size[0] >= 150 {
			m.help = false
			m.view = viewPods
			updated, _ := m.handleKey(keyMessage("tab"))
			m = updated.(appModel)
			if m.focus != focusDetail {
				t.Fatalf("wide detail focus did not open at %dx%d", size[0], size[1])
			}
		}
	}
}

func TestConnectionFailureAndRecoveryKeepSelection(t *testing.T) {
	m := loadedFixtureModel(t, 120, 30)
	m.view = viewPods
	m.currentViewport().move(1)
	want := m.selectedEntityKey()

	updated, _ := m.Update(fetchMsg{generation: m.fetchGeneration, err: errors.New("network unavailable")})
	m = updated.(appModel)
	if got := m.selectedEntityKey(); got != want {
		t.Fatalf("failure selection = %q, want %q", got, want)
	}

	reader := tuiFixtureReader()
	updated, _ = m.Update(fetchMsg{generation: m.fetchGeneration, data: snapshotData{
		Namespaces: reader.current.Namespaces,
		Workloads:  reader.current.Workloads,
		Pods:       reader.current.Pods,
		Containers: reader.current.Containers,
	}})
	m = updated.(appModel)
	if got := m.selectedEntityKey(); got != want {
		t.Fatalf("recovery selection = %q, want %q", got, want)
	}
}

func TestNoColorDisablesEveryThemeColour(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	theme := currentTheme()
	styles := map[string]lipgloss.Style{
		"accent": theme.accent, "healthy": theme.healthy, "warning": theme.warning,
		"danger": theme.danger, "muted": theme.muted, "error": theme.error,
		"help": theme.help, "selected": theme.selected,
	}
	for name, style := range styles {
		_, foregroundDisabled := style.GetForeground().(lipgloss.NoColor)
		_, backgroundDisabled := style.GetBackground().(lipgloss.NoColor)
		if !foregroundDisabled || !backgroundDisabled {
			t.Fatalf("NO_COLOR theme retained colour in %s", name)
		}
	}
	if theme.border != "" || theme.focus != "" {
		t.Fatal("NO_COLOR theme retained border or focus colour")
	}

	m := loadedFixtureModel(t, 180, 50)
	frame := m.viewString()
	for _, want := range []string{"A/F/S/O", "RISK", "›", "Diagnosis"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("monochrome frame missing semantic marker %q:\n%s", want, frame)
		}
	}
}

func TestUnicodeAndDoubleWidthTextStayWithinColumns(t *testing.T) {
	values := []string{
		"支付-api-界界界",
		"emoji-👩\u200d💻-pod",
		"combining-e\u0301-value",
	}
	for _, value := range values {
		for width := 1; width <= 16; width++ {
			got := truncate(value, width)
			if !utf8.ValidString(got) || lipgloss.Width(got) > width {
				t.Fatalf("truncate(%q, %d) = %q width %d", value, width, got, lipgloss.Width(got))
			}
			padded := pad(value, width, false)
			if !utf8.ValidString(padded) || lipgloss.Width(padded) != width {
				t.Fatalf("pad(%q, %d) = %q width %d", value, width, padded, lipgloss.Width(padded))
			}
		}
	}

	m := loadedFixtureModel(t, 80, 24)
	m.data.Pods[0].PodName = values[0]
	m.data.Pods[1].PodName = values[1]
	m.data.Pods[1].Context.WorkloadName = values[2]
	for _, size := range [][2]int{{80, 24}, {120, 30}, {180, 50}} {
		m.width, m.height = size[0], size[1]
		m.resizeViewports()
		assertFrameBounds(t, m.viewString(), size[0], size[1])
	}
}

func TestColouredErrorHeaderRemainsColumnBounded(t *testing.T) {
	previous, existed := os.LookupEnv("NO_COLOR")
	if err := os.Unsetenv("NO_COLOR"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if existed {
			_ = os.Setenv("NO_COLOR", previous)
		} else {
			_ = os.Unsetenv("NO_COLOR")
		}
	})

	m := newModel(context.Background(), Options{}, tuiFixtureReader(), "test")
	m.width, m.height = 40, 10
	m.statusErr = errors.New("a deliberately long connection failure")
	m.loading = false
	header := m.renderHeader(40)
	if lipgloss.Width(header) > 40 || strings.Contains(header, "\x1b[") {
		t.Fatalf("header width=%d contains embedded style before truncation: %q", lipgloss.Width(header), header)
	}
}

func assertFrameBounds(t *testing.T, frame string, width, height int) {
	t.Helper()
	lines := strings.Split(frame, "\n")
	if len(lines) > height {
		t.Fatalf("frame height=%d, want <=%d:\n%s", len(lines), height, frame)
	}
	for index, line := range lines {
		if got := lipgloss.Width(line); got > width {
			t.Fatalf("line %d width=%d, want <=%d: %q", index+1, got, width, line)
		}
	}
}
