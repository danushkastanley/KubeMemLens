package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/client"
	"github.com/danushkastanley/kube-memlens/internal/incidentsession"
	"github.com/rivo/uniseg"
)

type sessionPanelReader struct {
	*fakeSnapshotReader
	open func(string) (client.IncidentSessions, error)
}

func (r *sessionPanelReader) OpenIncidentSessions(namespace string) (client.IncidentSessions, error) {
	return r.open(namespace)
}

type panelSessionClient struct {
	client.IncidentSessions
	start    func(context.Context) (incidentsession.Summary, error)
	annotate func(context.Context, string, string) (incidentsession.Summary, error)
	closed   int
}

func (c *panelSessionClient) StartIncident(ctx context.Context) (incidentsession.Summary, error) {
	return c.start(ctx)
}
func (c *panelSessionClient) AnnotateIncident(ctx context.Context, id, note string) (incidentsession.Summary, error) {
	return c.annotate(ctx, id, note)
}
func (c *panelSessionClient) Close() { c.closed++ }

func sessionModel(t *testing.T, connection client.IncidentSessions) *appModel {
	t.Helper()
	m := panelModel(t)
	m.opts.Namespace = "team-a"
	m.data.Pods = []api.PodSnapshot{{Namespace: "team-a", PodName: "app", PodUID: "pod-uid", CapturedAt: time.Now().UTC()}}
	m.client = &sessionPanelReader{fakeSnapshotReader: tuiFixtureReader(), open: func(namespace string) (client.IncidentSessions, error) {
		if namespace != "team-a" {
			t.Errorf("wrong namespace %q", namespace)
		}
		return connection, nil
	}}
	return m
}

func sessionKey(t *testing.T, m *appModel, key string) tea.Cmd {
	t.Helper()
	updated, command := m.handleKey(keyMessage(key))
	*m = updated.(appModel)
	return command
}

func TestSessionPanelExplicitStartAndPrivateInput(t *testing.T) {
	calls := 0
	connection := &panelSessionClient{start: func(context.Context) (incidentsession.Summary, error) {
		calls++
		return incidentsession.Summary{ID: strings.Repeat("a", 32)}, nil
	}}
	m := sessionModel(t, connection)
	if command := sessionKey(t, m, "I"); command != nil || !m.sessionPanel.open || calls != 0 {
		t.Fatal("panel opening created a session")
	}
	command := sessionKey(t, m, "n")
	if command == nil || !m.sessionPanel.loading {
		t.Fatal("explicit start not submitted")
	}
	m.receiveSessionPanel(command().(sessionPanelMsg))
	if calls != 1 || m.sessionPanel.id == "" {
		t.Fatal("start acknowledgement lost")
	}
	sessionKey(t, m, "t")
	updated, _ := m.Update(tea.PasteMsg{Content: "private decision"})
	*m = updated.(appModel)
	if m.sessionPanel.input != "private decision" {
		t.Fatal("annotation input lost")
	}
	if strings.Contains(m.viewString(), "private decision") || strings.Contains(fmt.Sprintf("%#v", m.sessionPanel), "private decision") {
		t.Fatal("private input exposed")
	}
	connection.annotate = func(_ context.Context, id, note string) (incidentsession.Summary, error) {
		if note != "private decision" || id != m.sessionPanel.id {
			t.Error("annotation binding changed")
		}
		return incidentsession.Summary{ID: id}, nil
	}
	command = sessionKey(t, m, "enter")
	if command == nil || m.sessionPanel.input != "" {
		t.Fatal("submitted input retained in model")
	}
	m.receiveSessionPanel(command().(sessionPanelMsg))
	sessionKey(t, m, "t")
	sessionKey(t, m, "secret")
	sessionKey(t, m, "esc")
	if m.sessionPanel.input != "" || m.sessionPanel.mode != sessionMenu {
		t.Fatal("dismissed annotation retained")
	}
}

func TestSessionPanelCancelsAndRejectsLateMutation(t *testing.T) {
	started, cancelled := make(chan struct{}), make(chan struct{})
	connection := &panelSessionClient{start: func(ctx context.Context) (incidentsession.Summary, error) {
		close(started)
		<-ctx.Done()
		close(cancelled)
		return incidentsession.Summary{ID: strings.Repeat("b", 32)}, nil
	}}
	m := sessionModel(t, connection)
	sessionKey(t, m, "I")
	command := sessionKey(t, m, "n")
	done := make(chan sessionPanelMsg, 1)
	go func() { done <- command().(sessionPanelMsg) }()
	<-started
	if another := sessionKey(t, m, "n"); another != nil {
		t.Fatal("duplicate in-flight mutation queued")
	}
	sessionKey(t, m, "esc")
	<-cancelled
	m.receiveSessionPanel(<-done)
	if m.sessionPanel.open || m.sessionPanel.id != "" || connection.closed != 1 {
		t.Fatal("cancelled panel accepted late state")
	}
	if m.action.err == nil || !strings.Contains(m.action.err.Error(), "may have completed") {
		t.Fatal("interruption incorrectly promised rollback")
	}
}

func TestSessionPanelRevocationAndNamespaceChangeClearState(t *testing.T) {
	connection := &panelSessionClient{}
	m := sessionModel(t, connection)
	sessionKey(t, m, "I")
	m.sessionPanel.id = strings.Repeat("a", 32)
	m.sessionPanel.input = "private"
	m.sessionPanel.lines = []string{"private report"}
	generation := m.sessionPanel.generation
	m.clearRevokedData()
	m.receiveSessionPanel(sessionPanelMsg{generation: generation, summary: incidentsession.Summary{ID: strings.Repeat("a", 32)}})
	if m.sessionPanel.id != "" || m.sessionPanel.input != "" || len(m.sessionPanel.lines) != 0 {
		t.Fatal("revoked session state retained")
	}
	m = sessionModel(t, connection)
	m.sessionPanel.namespace = "team-b"
	m.sessionPanel.id = strings.Repeat("b", 32)
	if command := m.openSessionPanel(); command != nil || m.sessionPanel.id != "" || m.sessionPanel.namespace != "team-a" {
		t.Fatal("old namespace session reused")
	}
	m.clearSessionPanel()
}

func TestSessionPanelInputBoundsAndSmallLayouts(t *testing.T) {
	m := sessionModel(t, &panelSessionClient{})
	sessionKey(t, m, "I")
	sessionKey(t, m, "t")
	for _, text := range []string{strings.Repeat("x", 513), "line\nbreak", "\x1b[31m", "\u202eprivate"} {
		m.appendSessionInput(text)
		if m.sessionPanel.input != "" || m.sessionPanel.err == nil {
			t.Fatal("unsupported annotation accepted")
		}
	}
	m.appendSessionInput("valid")
	for _, size := range [][2]int{{40, 10}, {40, 16}, {80, 24}, {120, 40}} {
		m.width, m.height = size[0], size[1]
		m.resizeViewports()
		frame := m.viewString()
		if len(strings.Split(frame, "\n")) > m.height {
			t.Fatal("session panel overflowed terminal height")
		}
		for _, line := range strings.Split(frame, "\n") {
			if uniseg.StringWidth(line) > m.width {
				t.Fatal("session panel overflowed width")
			}
		}
	}
	sessionKey(t, m, "esc")
	sessionKey(t, m, "esc")
	updated, _ := m.Update(tea.PasteMsg{Content: "n"})
	*m = updated.(appModel)
	if m.sessionPanel.open || m.sessionPanel.input != "" {
		t.Fatal("paste outside panel changed session state")
	}
}

func TestSessionExpiryClearsDisplayAndRejectsInFlightResults(t *testing.T) {
	m := sessionModel(t, &panelSessionClient{})
	sessionKey(t, m, "I")
	at := time.Now().UTC()
	m.sessionPanel.summary = incidentsession.Summary{ID: strings.Repeat("a", 32), ExpiresAt: at}
	m.sessionPanel.id = m.sessionPanel.summary.ID
	m.sessionPanel.input = "private"
	m.sessionPanel.lines = []string{"private evidence"}
	ctx, cancel := context.WithCancel(t.Context())
	m.sessionPanel.cancel = cancel
	generation := m.sessionPanel.generation
	m.expireSession(at)
	if ctx.Err() == nil || m.sessionPanel.id != "" || m.sessionPanel.input != "" || len(m.sessionPanel.lines) != 0 || !m.sessionPanel.open {
		t.Fatal("expiry did not clear local evidence")
	}
	m.receiveSessionPanel(sessionPanelMsg{generation: generation, summary: incidentsession.Summary{ID: strings.Repeat("a", 32)}})
	if m.sessionPanel.id != "" {
		t.Fatal("expired session restored by late message")
	}
	m.clearSessionPanel()
}

func TestSessionLongInputKeepsCaretVisibleWithoutExposingNote(t *testing.T) {
	m := sessionModel(t, &panelSessionClient{})
	sessionKey(t, m, "I")
	m.sessionPanel.id = strings.Repeat("a", 32)
	sessionKey(t, m, "t")
	m.appendSessionInput(strings.Repeat("private", 70))
	m.width, m.height = 40, 10
	m.resizeViewports()
	frame := m.viewString()
	if strings.Contains(frame, "private") || !strings.Contains(frame, "█") || !strings.Contains(frame, m.sessionPanel.id) {
		t.Fatal("masked input caret or session binding is not visible")
	}
	m.clearSessionPanel()
}
