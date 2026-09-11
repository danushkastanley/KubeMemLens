package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/observationview"
)

func (m *appModel) showObservationRecommendations() {
	ref, ok := m.currentActionRef()
	if !ok {
		m.setActionError(fmt.Errorf("select an observation first"))
		return
	}
	row, ok := m.observationForRef(ref)
	if !ok {
		m.setActionError(fmt.Errorf("the selected observation is no longer available"))
		return
	}
	m.action.mode, m.action.err = actionResultMode, nil
	lines := append([]string{"Automatic mutation: disabled"}, observationview.Recommendations(row)...)
	lines = append(lines, "")
	lines = append(lines, observationview.Summary(row, time.Now())...)
	m.action.result = actionResult{title: "Restricted read-only recommendations", lines: lines}
}

func (m *appModel) beginCapture() {
	if m.restricted() {
		m.setActionError(fmt.Errorf("restricted capture is unavailable in this build; no file has been written"))
		return
	}
	m.action.mode, m.action.input, m.action.err = actionCapturePath, "", nil
}

func (m appModel) observationCommand() (string, bool) {
	ref, ok := m.currentActionRef()
	if !ok {
		return "", false
	}
	row, ok := m.observationForRef(ref)
	if !ok {
		return "", false
	}
	command, ok := observationview.Command(row)
	if !ok {
		return "", false
	}
	for _, option := range []struct{ flag, value string }{
		{"--kubeconfig", m.opts.ConnectionOptions.Kubeconfig}, {"--context", m.opts.ConnectionOptions.Context},
	} {
		if option.value != "" {
			command += " " + option.flag + " '" + strings.ReplaceAll(option.value, "'", "'\"'\"'") + "'"
		}
	}
	return command, true
}
