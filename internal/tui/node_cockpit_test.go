package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/capability"
	"github.com/danushkastanley/kube-memlens/internal/client"
	"github.com/danushkastanley/kube-memlens/internal/incident"
	"github.com/danushkastanley/kube-memlens/internal/nodeanalysis"
)

func TestNodeDetailScrollExposesAllFieldsWithoutRefetch(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	for _, size := range [][2]int{{80, 24}, {100, 30}, {160, 35}} {
		m, r := loadedCockpitModel(t, size[0], size[1])
		m.openSelectedDetail()
		before := r.analysisCalls.Load()
		if m.view != viewDetail {
			t.Fatal("Node detail did not open")
		}
		var frames strings.Builder
		for line := 0; line < len(m.detailLines(m.width)); line++ {
			frame := m.viewString()
			assertFrameBounds(t, frame, m.width, m.height)
			frames.WriteString(frame)
			frames.WriteByte('\n')
			updated, cmd := m.Update(keyMessage("j"))
			m = updated.(appModel)
			if cmd != nil {
				t.Fatal("scroll scheduled a new request")
			}
		}
		for _, want := range []string{"Node cockpit", "Source completeness: partial", "Node memory", "Usage:", "Available:", "Working set:", "RSS:", "Swap usage:", "Swap available:", "PSI some avg10/60/300", "PSI full cumulative stall", "Major page faults", "Capacity:", "Allocatable:", "MemoryPressure:", "hugepages-2Mi", "kubelet started", "runtime started", "misc started", "pods started", "Observed Pod charge:", "Outside observed Pods estimate:", "Unaccounted estimate:", "private-team/Pod/private-pod", "Node history:", "Collector reset:", "kubectl memlens capture --node"} {
			if !strings.Contains(frames.String(), want) {
				t.Fatalf("%dx%d missing %q", size[0], size[1], want)
			}
		}
		for _, rune := range strings.Join(m.detailLines(m.width), "\n") {
			if rune > 127 {
				t.Fatalf("Node detail introduced non-ASCII content: %q", rune)
			}
		}
		if r.analysisCalls.Load() != before {
			t.Fatal("scroll polled Node source")
		}
	}
}

func TestWideNodePaneShowsIndependentMemoryAndContributors(t *testing.T) {
	m, _ := loadedCockpitModel(t, 160, 35)
	frame := m.viewString()
	assertFrameBounds(t, frame, 160, 35)
	for _, want := range []string{"Node usage:", "Observed Pod charge:", "Values overlap; do not stack.", "private-team/private-pod", "partial"} {
		if !strings.Contains(frame, want) {
			t.Fatal("wide pane missing", want, frame)
		}
	}
	updated, _ := m.Update(keyMessage("tab"))
	m = updated.(appModel)
	if m.focus != focusDetail {
		t.Fatal("wide Node detail did not receive focus")
	}
	updated, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(appModel)
	if m.focus != focusTable {
		t.Fatal("compact resize left hidden detail focused")
	}
}

func TestNodeDrillDownUsesExistingAuthorisedPodView(t *testing.T) {
	m, _ := loadedCockpitModel(t, 80, 24)
	m.data.Pods = []api.PodSnapshot{{Namespace: "team", PodName: "selected-node", NodeName: "node-a"}, {Namespace: "team", PodName: "other-node", NodeName: "node-b"}}
	m.openSelectedDetail()
	m.enter()
	if m.view != viewPods || m.currentNode != "node-a" || len(m.visiblePods()) != 1 || m.visiblePods()[0].PodName != "selected-node" || m.selectedNode.name != "" {
		t.Fatal("Node drill-down did not preserve scope/navigation")
	}
	m.back()
	if m.view != viewNodes {
		t.Fatal("Node drill-down back navigation lost parent")
	}
}

func TestNodeCaptureActionReadsFreshAndReauthorisesOverwrite(t *testing.T) {
	m, r := loadedCockpitModel(t, 80, 24)
	path := filepath.Join(t.TempDir(), "node.json")
	m.action.input = path
	before := r.analysisCalls.Load()
	cmd := m.startCapture(false)
	if cmd == nil {
		t.Fatal("Node capture action unavailable")
	}
	updated, _ := m.Update(cmd())
	m = updated.(appModel)
	if m.action.err != nil || r.analysisCalls.Load() != before+1 {
		t.Fatal("Node capture reused screen data", m.action.err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("capture permissions", err)
	}
	body, _ := os.ReadFile(path)
	if strings.Contains(string(body), "private-pod") || strings.Contains(string(body), "private-team") || strings.Contains(string(body), "private-node-uid") {
		t.Fatal("TUI capture retained raw identities")
	}
	doc, err := incident.Read(path)
	if err != nil || doc.Node == nil {
		t.Fatal("TUI capture not replayable", err)
	}
	cmd = m.startCapture(false)
	updated, _ = m.Update(cmd())
	m = updated.(appModel)
	if m.action.overwriteRequest == nil {
		t.Fatal("existing capture did not require overwrite confirmation")
	}
	r.analysisErr = &client.ReadError{Kind: client.ReadErrorForbidden}
	updated, cmd = m.Update(keyMessage("f"))
	m = updated.(appModel)
	if cmd == nil {
		t.Fatal("overwrite retry did not attempt current authorisation")
	}
	updated, _ = m.Update(cmd())
	m = updated.(appModel)
	if m.action.err == nil {
		t.Fatal("revoked overwrite succeeded")
	}
	after, _ := os.ReadFile(path)
	if string(body) != string(after) {
		t.Fatal("revoked overwrite replaced file")
	}
}

func TestNodeRequestCancellationOnSelectionAndQuit(t *testing.T) {
	for _, key := range []string{"p", "q"} {
		t.Run(key, func(t *testing.T) {
			m, r := loadedCockpitModel(t, 80, 24)
			r.started = make(chan string, 1)
			r.cancelled = make(chan string, 1)
			cmd := m.nodeRefreshCmd()
			done := make(chan tea.Msg, 1)
			go func() { done <- cmd() }()
			select {
			case <-r.started:
			case <-time.After(time.Second):
				t.Fatal("read did not start")
			}
			updated, _ := m.Update(keyMessage(key))
			m = updated.(appModel)
			select {
			case <-r.cancelled:
			case <-time.After(time.Second):
				t.Fatal("Node request not cancelled")
			}
			updated, _ = m.Update(<-done)
			m = updated.(appModel)
			if m.selectedNode.inFlight {
				t.Fatal("cancelled request remained active")
			}
		})
	}
}

func TestNodeCurrentCommandAndDisabledFallback(t *testing.T) {
	m, r := loadedCockpitModel(t, 80, 24)
	if command, ok := m.currentCommand(); !ok || command != "kubectl memlens explain node node-a" {
		t.Fatal("Node follow-up command", command)
	}
	r.analysisErr = &client.ReadError{Kind: client.ReadErrorNotFound}
	r.errorHistory = r.analysisErr
	cmd := m.nodeRefreshCmd()
	updated, _ := m.Update(cmd())
	m = updated.(appModel)
	m.openSelectedDetail()
	text := strings.Join(m.detailLines(80), "\n")
	if !strings.Contains(text, "observed-charge fallback") || !strings.Contains(text, "Summed Pod charge") || strings.Contains(text, "private-pod") {
		t.Fatal("disabled profile lost safe fallback", text)
	}
	_, err := (localActionExecutor{}).Run(context.Background(), actionRequest{kind: actionCapture, nodeReader: r, ref: entityRef{kind: entityNode, nodeName: "node-a"}, nodeRank: nodeanalysis.Total, outputPath: filepath.Join(t.TempDir(), "unavailable.json")})
	if err == nil {
		t.Fatal("disabled profile capture succeeded")
	}
	if m.selectedNode.evidence != nil && m.selectedNode.evidence.Record.Freshness == capability.Fresh {
		t.Fatal("disabled source still fresh")
	}
}
