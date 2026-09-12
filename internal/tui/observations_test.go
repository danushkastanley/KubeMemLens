package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/capability"
	"github.com/danushkastanley/kube-memlens/internal/client"
	"github.com/danushkastanley/kube-memlens/internal/observation"
	"github.com/danushkastanley/kube-memlens/internal/observationview"
	"github.com/rivo/uniseg"
)

type observationFixture struct {
	batch observation.Batch
	err   error
	calls int
}

func (f *observationFixture) Current(context.Context) (observation.Batch, error) {
	f.calls++
	return f.batch, f.err
}

func restrictedModel(t *testing.T) (appModel, *observationFixture) {
	t.Helper()
	now := time.Now().UTC()
	zero, large := uint64(0), uint64(32<<20)
	status := capability.Envelope{Source: capability.KubernetesStatus, APIVersion: "v1", ReceivedAt: now, Scope: capability.PodScope, Freshness: capability.Fresh, Completeness: capability.Complete, Stability: capability.Stable}
	batch := observation.Batch{Mode: capability.Restricted, ReceivedAt: now, Completeness: capability.Partial, Sources: []observation.SourceReport{{Scope: capability.PodScope, SourceState: capability.SourceState{Source: capability.KubernetesStatus, APIVersion: "v1", Availability: capability.Available, Freshness: capability.Fresh, Completeness: capability.Complete, Stability: capability.Stable}}}}
	for i, entry := range []struct {
		name  string
		value *uint64
	}{{"large", &large}, {"zero", &zero}, {"missing", nil}} {
		quantity := observation.WorkingSet{Bytes: entry.value, Availability: capability.Available, Coverage: observation.Coverage{Reported: 1, Expected: 1, Unit: capability.ContainerScope}, Evidence: capability.Envelope{
			Source: capability.KubernetesMetrics, APIVersion: "metrics.k8s.io/v1beta1", CapturedAt: now.Add(-10 * time.Second), ReceivedAt: now, Window: 15 * time.Second, Scope: capability.ContainerScope, Freshness: capability.Fresh, Completeness: capability.Complete, Stability: capability.Beta}}
		if entry.value == nil {
			quantity.Availability = capability.Unreported
			quantity.Evidence.Completeness = capability.Partial
			quantity.Coverage.Reported = 0
		}
		pod := observation.Pod{Namespace: "team-a", Name: entry.name, NodeName: "node-a", Context: api.PodContext{Phase: "Running", CreatedAt: now.Add(-time.Hour), WorkloadKind: "Deployment", WorkloadName: "api"}, OwnerAvailability: capability.Available, WorkingSet: quantity,
			Containers: []observation.Container{{Name: "worker", Kind: "application", State: "running", WorkingSet: quantity}}}
		if i == 2 {
			pod.Containers[0].State = "unreported"
		}
		pod.StatusEvidence, pod.OwnerEvidence = status, status
		pod.WorkingSet = observation.SumWorkingSets([]observation.WorkingSet{quantity}, capability.PodScope)
		batch.Pods = append(batch.Pods, pod)
	}
	batch.Namespaces, batch.Workloads = observation.GroupPods(batch.Pods)
	batch.Nodes = []observation.Node{{Name: "node-a", StatusAvailability: capability.Forbidden, MemoryPressure: "unreported", WorkingSet: observation.WorkingSet{Availability: capability.Forbidden}}}
	reader := &observationFixture{batch: batch}
	plan, err := capability.Plan(capability.Restricted, []capability.SourceState{{Source: capability.KubernetesStatus, Availability: capability.Available}})
	if err != nil {
		t.Fatal(err)
	}
	m := newModel(t.Context(), Options{Namespace: "team-a"}, nil, "")
	m.width, m.height = 80, 24
	m.resizeViewports()
	updated, cmd := m.Update(discoveryMsg{generation: m.fetchGeneration, session: client.EvidenceSession{Plan: plan, Observations: reader, Description: "Kubernetes APIs"}})
	m = updated.(appModel)
	if cmd == nil {
		t.Fatal("no current query after discovery")
	}
	updated, _ = m.Update(cmd())
	return updated.(appModel), reader
}

func TestRestrictedFramesUseWorkingSetsAtEverySize(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	m, _ := restrictedModel(t)
	for _, size := range [][2]int{{40, 10}, {80, 24}, {120, 40}, {180, 40}} {
		updated, _ := m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		m = updated.(appModel)
		frame := m.viewString()
		if !strings.Contains(frame, "restricted / Kubernetes APIs") || !strings.Contains(frame, "WORKING SET") || !strings.Contains(frame, "unreported") {
			t.Fatalf("%v: %s", size, frame)
		}
		for _, word := range []string{"RISK ORDER", "DIAGNOSIS", "OOM 0", "baseline", "CGROUP SIGNALS"} {
			if strings.Contains(frame, word) {
				t.Fatalf("restricted frame acquired %s", word)
			}
		}
		for _, line := range strings.Split(frame, "\n") {
			if uniseg.StringWidth(line) > size[0] {
				t.Fatalf("line wider than terminal: %q", line)
			}
		}
		if len(strings.Split(frame, "\n")) > size[1] {
			t.Fatal("frame taller than terminal")
		}
	}
	rows := m.visibleObservationRows()
	if rows[0].Name != "large" || rows[1].Name != "zero" || rows[2].Name != "missing" {
		t.Fatal("missing evidence used for risk or numeric ordering")
	}
}

func TestRestrictedNavigationSelectionAndUnavailableActions(t *testing.T) {
	m, reader := restrictedModel(t)
	for _, key := range []string{"n", "enter", "w", "enter", "c", "enter"} {
		updated, _ := m.Update(tea.KeyPressMsg{Text: key, Code: []rune(key)[0]})
		if key == "enter" {
			updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		}
		m = updated.(appModel)
	}
	if m.view != viewDetail || m.detail.kind != entityContainer || !strings.Contains(strings.Join(m.detailLines(80), "\n"), "deep evidence is required") {
		t.Fatal("drill did not retain source-aware detail")
	}
	if m.ensureHistoryTarget() != nil || m.historyRefreshCmd() != nil || m.beginCompleteFetch() != nil {
		t.Fatal("restricted detail requested cgroup history or detail")
	}
	if m.startRecommendation() != nil || !strings.Contains(m.action.result.title, "Restricted") {
		t.Fatal("restricted recommendations escaped source boundary")
	}
	m.beginCapture()
	if m.action.mode != actionCapturePath || m.action.err != nil {
		t.Fatal("capture did not request an explicit destination")
	}
	if m.startCompare() != nil || m.action.err != nil || m.action.observationSource == nil {
		t.Fatal("comparison did not mark the source")
	}
	if reader.calls != 1 {
		t.Fatal("navigation made hidden API reads")
	}
}

func TestRestrictedRefreshKeepsTransientFrameAndClearsRevokedData(t *testing.T) {
	m, reader := restrictedModel(t)
	m.currentViewport().selected = 1
	key := m.selectedEntityKey()
	reader.err = errors.New("transport unavailable")
	updated, _ := m.Update(m.fetchCmd()())
	m = updated.(appModel)
	if !m.hasData() || m.selectedEntityKey() != key || m.lastRefresh.IsZero() {
		t.Fatal("transient failure discarded current frame")
	}
	reader.err = nil
	updated, _ = m.Update(m.fetchCmd()())
	m = updated.(appModel)
	if m.statusErr != nil || m.selectedEntityKey() != key {
		t.Fatal("recovery lost selection")
	}
	reader.err = &capability.SelectionError{Mode: capability.Restricted, Reason: capability.AccessDenied}
	updated, _ = m.Update(m.fetchCmd()())
	m = updated.(appModel)
	if m.hasData() || len(m.data.ObservationRows) != 0 || !m.lastRefresh.IsZero() {
		t.Fatal("revoked namespace data retained")
	}
	if strings.Contains(m.viewString(), "team-a/large") {
		t.Fatal("revoked identity rendered")
	}
	reader.err = nil
	updated, _ = m.Update(m.fetchCmd()())
	m = updated.(appModel)
	if !m.hasData() || m.statusErr != nil {
		t.Fatal("reader did not recover after permission restoration")
	}
}

func TestRestrictedPauseSortFiltersAndChangingMetadata(t *testing.T) {
	m, reader := restrictedModel(t)
	m.paused = true
	_, _ = m.Update(tickMsg(time.Now()))
	if reader.calls != 1 {
		t.Fatal("pause issued a query")
	}
	m.cycleSort()
	if m.sort != sortName {
		t.Fatal("unavailable sort retained")
	}
	m.cycleSort()
	if m.sort != sortTotal {
		t.Fatal("working-set order not restored")
	}
	m.query = "owner:api state:fresh"
	if len(m.visibleObservationRows()) != 2 {
		t.Fatal("supported filter lost")
	}
	m.query = "pressure:false"
	if !strings.Contains(m.renderObservationTable(80), "require deep evidence") {
		t.Fatal("missing PSI treated as clean")
	}
	m.query = ""
	ref, _ := m.currentEntityRef()
	changed := m.data.ObservationRows
	for i := range changed {
		if changed[i].Scope == capability.PodScope {
			changed[i].NodeName = "replacement-node"
		}
	}
	m.data.ObservationRows = changed
	if _, ok := m.observationForRef(ref); !ok {
		t.Fatal("selection identity depended on mutable Node metadata")
	}
	if observationview.State(m.visibleObservationRows()[0], time.Now().Add(3*time.Minute)) != "stale" {
		t.Fatal("paused sample never aged")
	}
}

func TestRestrictedHelpAndFailureAgeRemainVisibleInCompactFrames(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	m, _ := restrictedModel(t)
	m.width, m.height = 40, 10
	m.resizeViewports()
	m.statusErr = &capability.SelectionError{Mode: capability.Restricted, Reason: capability.RequestFailed}
	frame := m.viewString()
	if !strings.Contains(frame, "state: unavailable") || !strings.Contains(frame, "old") {
		t.Fatalf("compact failure lost state or sample age: %s", frame)
	}
	m.statusErr = nil
	m.paused = true
	if !strings.Contains(m.viewString(), "paused") {
		t.Fatal("compact pause hidden")
	}
	m.help = true
	for _, size := range [][2]int{{40, 10}, {80, 24}, {180, 40}} {
		m.width, m.height = size[0], size[1]
		m.resizeViewports()
		frame := m.viewString()
		if len(strings.Split(frame, "\n")) > size[1] {
			t.Fatal("help escaped the terminal height")
		}
		if size[0] >= 80 && !strings.Contains(frame, "cluster policy") {
			t.Fatal("help promised deep access without policy context")
		}
	}
}

func TestRestrictedCopiedCommandPreservesExplicitCallerOptions(t *testing.T) {
	m, _ := restrictedModel(t)
	m.opts.ConnectionOptions.Kubeconfig = "/private/test's config"
	m.opts.ConnectionOptions.Context = "selected-context"
	command, ok := m.currentCommand()
	if !ok || !strings.Contains(command, `--kubeconfig '/private/test'"'"'s config'`) || !strings.Contains(command, "--context 'selected-context'") {
		t.Fatalf("caller options missing or unquoted: %q", command)
	}
}

func TestRestrictedManualRefreshStillWorksWhilePaused(t *testing.T) {
	m, reader := restrictedModel(t)
	m.paused = true
	updated, command := m.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	m = applyCommand(t, updated.(appModel), command)
	if reader.calls != 2 || !m.paused || m.loading {
		t.Fatal("manual paused refresh did not complete")
	}
}

func TestRestrictedStateColumnFitsWithoutTruncation(t *testing.T) {
	m, _ := restrictedModel(t)
	for _, width := range []int{40, 70, 80, 108, 180} {
		lines := strings.Split(m.renderObservationTable(width), "\n")
		if !strings.HasSuffix(strings.TrimSpace(lines[0]), "STATE") || !strings.HasSuffix(strings.TrimSpace(lines[len(lines)-1]), "unreported") {
			t.Fatalf("state column clipped at %d: %s", width, strings.Join(lines, "\n"))
		}
	}
}
