package tui

import tea "charm.land/bubbletea/v2"

// quit is the one way out of the dashboard. It stops every request that is
// still running, whichever screen the key was pressed on.
func (m appModel) quit() (tea.Model, tea.Cmd) {
	m.clearSessionPanel()
	m.closeHistoryPanel()
	m.closeReplicaPanel()
	m.cancelHistoryRequest()
	m.cancelNodeRequest()
	m.cancelVolumeRequest()
	return m, tea.Quit
}
