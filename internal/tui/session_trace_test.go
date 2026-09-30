package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/client"
	"github.com/danushkastanley/kube-memlens/internal/incidentsession"
	"github.com/danushkastanley/kube-memlens/internal/trace"
	"github.com/danushkastanley/kube-memlens/internal/traceclient"
	"github.com/danushkastanley/kube-memlens/internal/tracereport"
)

type referenceSessionClient struct {
	client.IncidentSessions
	path  string
	data  []byte
	calls int
	err   error
}

func (c *referenceSessionClient) Close() {}
func (c *referenceSessionClient) ReferenceIncidentTrace(_ context.Context, _ string, data []byte) (incidentsession.Summary, error) {
	c.calls++
	file, err := os.ReadFile(c.path)
	if err != nil || string(file) != string(data) {
		return incidentsession.Summary{}, errors.New("report was not saved before reference submission")
	}
	c.data = append([]byte(nil), data...)
	return incidentsession.Summary{ID: strings.Repeat("a", 32), Latest: incidentsession.EntryStatus{Kind: incidentsession.TraceReferenced}}, c.err
}

func terminalTraceSnapshot() traceclient.Snapshot {
	return traceclient.Snapshot{State: traceclient.StateFailed, Intent: traceclient.DefaultIntent(trace.Files), Cleanup: traceclient.CleanupUnconfirmed}
}

func TestSessionTraceSavesExactReportBeforeReferencing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.json")
	c := &referenceSessionClient{path: path}
	m := sessionModel(t, c)
	sessionKey(t, m, "I")
	m.sessionPanel.id = strings.Repeat("a", 32)
	m.tracePanel.target = api.ContainerSnapshot{Namespace: m.sessionPanel.namespace}
	m.tracePanel.snapshot = terminalTraceSnapshot()
	sessionKey(t, m, "T")
	if m.sessionPanel.mode != sessionTracePath {
		t.Fatal("path selection not opened")
	}
	m.sessionPanel.input = path
	if m.submitSessionInput() != nil || m.sessionPanel.mode != sessionTraceConfirm || c.calls != 0 {
		t.Fatal("path selection submitted without confirmation")
	}
	m.sessionPanel.input = "attach"
	command := m.submitSessionInput()
	if command == nil {
		t.Fatal("confirmed reference not scheduled", m.sessionPanel.err)
	}
	result := command().(sessionPanelMsg)
	if result.err != nil || c.calls != 1 {
		t.Fatal("reference failed", result.err)
	}
	ref, err := tracereport.Describe(c.data)
	if err != nil || ref.Coverage != "unreported" || ref.Cleanup != traceclient.CleanupUnconfirmed {
		t.Fatal("missing trace evidence became complete", err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatal("report file was not private", err)
	}
	if m.sessionPanel.tracePath != "" {
		t.Fatal("submitted path retained in input state")
	}
}

func TestSessionTraceRejectsChangedRevokedOrForeignTrace(t *testing.T) {
	for _, mode := range []string{"changed", "revoked", "foreign", "running"} {
		t.Run(mode, func(t *testing.T) {
			c := &referenceSessionClient{path: filepath.Join(t.TempDir(), "report.json")}
			m := sessionModel(t, c)
			sessionKey(t, m, "I")
			m.sessionPanel.id = strings.Repeat("a", 32)
			m.tracePanel.target = api.ContainerSnapshot{Namespace: m.sessionPanel.namespace}
			m.tracePanel.snapshot = terminalTraceSnapshot()
			sessionKey(t, m, "T")
			if m.sessionPanel.mode != sessionTracePath {
				t.Fatal("trace selection not opened", m.sessionPanel.err)
			}
			m.sessionPanel.tracePath = c.path
			m.sessionPanel.mode = sessionTraceConfirm
			m.sessionPanel.input = "attach"
			switch mode {
			case "changed":
				m.tracePanel.generation++
			case "revoked":
				m.tracePanel.revoked = true
			case "foreign":
				m.tracePanel.target.Namespace = "another-namespace"
			case "running":
				m.tracePanel.snapshot.State = traceclient.StateRunning
			}
			if m.submitSessionInput() != nil || m.sessionPanel.err == nil || c.calls != 0 {
				t.Fatal("stale or ineligible trace submitted")
			}
			if _, err := os.Stat(c.path); !os.IsNotExist(err) {
				t.Fatal("ineligible trace wrote a file")
			}
		})
	}
}

func TestSessionTracePreservesLocalFileOnUncertainAttachment(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.json")
	c := &referenceSessionClient{path: path, err: &client.IncidentError{OutcomeUnknown: true}}
	doc, err := tracereport.New(terminalTraceSnapshot(), "test", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	request := sessionRequest{operation: sessionReferenceTrace, id: strings.Repeat("a", 32), input: path, traceDocument: doc}
	result := runSessionWork(context.Background(), c, request)
	if result.err == nil || !strings.Contains(result.err.Error(), "saved locally") || c.calls != 1 {
		t.Fatal("uncertain mutation became success or retry")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("retained local report lost", err)
	}
	result = runSessionWork(context.Background(), c, request)
	if result.err == nil || c.calls != 1 {
		t.Fatal("existing report overwritten or duplicate reference sent")
	}
}
