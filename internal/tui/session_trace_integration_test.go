package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/incident"
	"github.com/danushkastanley/kube-memlens/internal/incidentsession"
	"github.com/danushkastanley/kube-memlens/internal/tracereport"
)

func TestSessionTraceReferenceTLSFileAndExportWorkflow(t *testing.T) {
	m, requests := liveSessionModel(t)
	completeSessionKey(t, m, "I")
	completeSessionKey(t, m, "n")
	m.tracePanel.target = api.ContainerSnapshot{Namespace: m.sessionPanel.namespace}
	m.tracePanel.snapshot = terminalTraceSnapshot()
	completeSessionKey(t, m, "T")
	path := filepath.Join(t.TempDir(), "report.json")
	pasteSession(t, m, path)
	completeSessionKey(t, m, "enter")
	if requests.Load() != 1 || m.sessionPanel.mode != sessionTraceConfirm {
		t.Fatal("path selection sent an implicit reference")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("report saved without confirmation")
	}
	pasteSession(t, m, "attach")
	completeSessionKey(t, m, "enter")
	if requests.Load() != 2 || m.sessionPanel.summary.Latest.Kind != incidentsession.TraceReferenced {
		t.Fatal("reference acknowledgement missing")
	}
	completeSessionKey(t, m, "r")
	lines := strings.Join(m.sessionPanel.lines, "\n")
	if !strings.Contains(lines, "trace-1") || !strings.Contains(lines, "unverified") || !strings.Contains(lines, "coverage unreported") {
		t.Fatal("trace provenance or missing evidence hidden", lines)
	}
	exportPath := filepath.Join(t.TempDir(), "incident.json")
	completeSessionKey(t, m, "E")
	pasteSession(t, m, exportPath)
	completeSessionKey(t, m, "enter")
	document, err := incident.ReadSession(exportPath)
	if err != nil || document.Authorised == nil {
		t.Fatal("authorised timeline export failed", err)
	}
	ref, err := incidentsession.SelectTraceReference(*document.Authorised, "trace-1")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || tracereport.VerifyReference(ref, data) != nil {
		t.Fatal("TUI did not preserve exact referenced report", err)
	}
	completeSessionKey(t, m, "d")
	pasteSession(t, m, "delete")
	completeSessionKey(t, m, "enter")
	if m.sessionPanel.id != "" || requests.Load() != 5 {
		t.Fatal("session deletion or mutation count differs")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("session deletion removed operator-owned report", err)
	}
}
