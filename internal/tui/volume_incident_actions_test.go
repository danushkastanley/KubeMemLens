package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/client"
	"github.com/danushkastanley/kube-memlens/internal/incident"
)

func TestVolumeCaptureActionReauthorisesOverwrite(t *testing.T) {
	m, r := volumeTUIFixture(t, 80, 24)
	path := filepath.Join(t.TempDir(), "volume.json")
	m.action.input = path
	before := r.calls
	cmd := m.startCapture(false)
	if cmd == nil {
		t.Fatal("capture unavailable")
	}
	m.completeAction(cmd().(actionMsg))
	if m.action.err != nil || r.calls != before+1 {
		t.Fatal("capture reused screen data", m.action.err)
	}
	doc, err := incident.Read(path)
	if err != nil || doc.Volume == nil || !doc.Volume.Redacted {
		t.Fatal("volume action wrote wrong incident domain", err)
	}
	body, _ := os.ReadFile(path)
	for _, marker := range []string{`"tenant"`, `"scratch"`, `"runtime"`} {
		if strings.Contains(string(body), marker) {
			t.Fatal("identity leaked into default capture")
		}
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatal("capture mode")
	}
	cmd = m.startCapture(false)
	m.completeAction(cmd().(actionMsg))
	if m.action.overwriteRequest == nil {
		t.Fatal("overwrite confirmation missing")
	}
	r.volumeErr = &client.ReadError{Kind: client.ReadErrorForbidden}
	updated, cmd := m.Update(keyMessage("f"))
	m = updated.(appModel)
	if cmd == nil {
		t.Fatal("overwrite did not attempt fresh read")
	}
	m.completeAction(cmd().(actionMsg))
	after, _ := os.ReadFile(path)
	if string(after) != string(body) || !client.IsForbidden(m.action.err) || m.selectedVolumes.context != nil {
		t.Fatal("revoked overwrite changed file or retained protected data")
	}
}

func TestVolumeCompareRechecksMarkedScopeAndInvalidatesLateActions(t *testing.T) {
	m, r := volumeTUIFixture(t, 80, 24)
	cmd := m.startVolumeCompare()
	m.completeAction(cmd().(actionMsg))
	if m.action.volumeCompareSource == nil {
		t.Fatal("comparison source not marked")
	}
	r.volumeErr = &client.ReadError{Kind: client.ReadErrorForbidden}
	cmd = m.startVolumeCompare()
	m.completeAction(cmd().(actionMsg))
	if !client.IsForbidden(m.action.err) || m.action.volumeCompareSource != nil || m.selectedVolumes.context != nil {
		t.Fatal("revoked comparison disclosed cached source")
	}
	m, r = volumeTUIFixture(t, 80, 24)
	cmd = m.startVolumeCompare()
	oldID := m.action.activeID
	m.invalidateActions()
	m.startVolumeCompare()
	newID := m.action.activeID
	m.completeAction(actionMsg{id: oldID, result: actionResult{title: "private-stale-result"}})
	if oldID == newID || m.action.result.title == "private-stale-result" {
		t.Fatal("action ID reused after revocation")
	}
	_ = cmd
}

func TestVolumeComparisonMarkExpiresIndependently(t *testing.T) {
	m, _ := volumeTUIFixture(t, 80, 24)
	cmd := m.startVolumeCompare()
	m.completeAction(cmd().(actionMsg))
	before := m.action.volumeCompareSource
	if before == nil {
		t.Fatal("comparison source missing")
	}
	m.selectedVolumes.context = nil
	m.expireVolumes(before.Now.Add(3 * time.Minute))
	if m.action.volumeCompareSource != nil {
		t.Fatal("comparison mark survived retention bound")
	}
}
