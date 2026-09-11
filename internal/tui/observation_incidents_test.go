package tui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/capability"
	"github.com/danushkastanley/kube-memlens/internal/incident"
)

func TestRestrictedTUICaptureAndOverwriteRetainOriginalFrame(t *testing.T) {
	m, _ := restrictedModel(t)
	path := filepath.Join(t.TempDir(), "capture.json")
	m.action.input = path
	cmd := m.startCapture(false)
	if cmd == nil {
		t.Fatalf("capture did not start: %v", m.action.err)
	}
	m.completeAction(cmd().(actionMsg))
	if m.action.err != nil {
		t.Fatal(m.action.err)
	}
	doc, err := incident.Read(path)
	if err != nil || doc.Restricted == nil || len(doc.Restricted.Observations.Pods) != 1 {
		t.Fatalf("invalid TUI capture: %v", err)
	}
	cmd = m.startCapture(false)
	m.completeAction(cmd().(actionMsg))
	if m.action.overwriteRequest == nil || m.action.overwriteRequest.restricted == nil {
		t.Fatal("overwrite lost restricted source")
	}
	request := *m.action.overwriteRequest
	request.overwrite = true
	// A new refresh must not change the already reviewed overwrite request.
	m.data.Observations = nil
	result, err := localActionExecutor{}.Run(t.Context(), request)
	if err != nil || !strings.Contains(result.title, "written") {
		t.Fatalf("overwrite: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("capture mode changed")
	}
}

func TestRestrictedTUICompareAndReadFailures(t *testing.T) {
	m, _ := restrictedModel(t)
	if m.startCompare() != nil || m.action.observationSource == nil {
		t.Fatal("source not marked")
	}
	m.currentViewport().selected = 1
	cmd := m.startCompare()
	if cmd == nil {
		t.Fatal("comparison not started")
	}
	m.completeAction(cmd().(actionMsg))
	if m.action.err != nil || !strings.Contains(strings.Join(m.action.result.lines, "\n"), "Working set: 32Mi → 0") {
		t.Fatalf("comparison: %v %#v", m.action.err, m.action.result)
	}
	for _, err := range []error{errors.New("transient read failed"), &capability.SelectionError{Mode: capability.Restricted, Reason: capability.AccessDenied}} {
		m.statusErr = err
		if m.startCompare() != nil || m.action.err == nil {
			t.Fatal("comparison started after read failure")
		}
		m.beginCapture()
		if m.action.mode == actionCapturePath || m.action.err == nil {
			t.Fatal("capture started after read failure")
		}
	}
}
