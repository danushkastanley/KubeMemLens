package tui

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/capability"
	"github.com/danushkastanley/kube-memlens/internal/client"
	"github.com/danushkastanley/kube-memlens/internal/nodeanalysis"
	"github.com/danushkastanley/kube-memlens/internal/nodecontext"
)

type cockpitReader struct {
	*fakeSnapshotReader
	evidence                    api.NodeEvidence
	nodeHistory                 api.NodeContextHistory
	analysisErr, errorHistory   error
	analysisCalls, historyCalls atomic.Int32
	started, cancelled          chan string
}

func (r *cockpitReader) NodeContext(context.Context, string) (api.NodeContextResource, error) {
	return api.NodeContextResource{Record: r.evidence.Record}, nil
}
func (r *cockpitReader) NodeAnalysis(ctx context.Context, name string, _ nodeanalysis.Metric, _ int) (nodeanalysis.Analysis, error) {
	r.analysisCalls.Add(1)
	if r.started != nil {
		r.started <- name
		<-ctx.Done()
		r.cancelled <- name
		return nodeanalysis.Analysis{}, ctx.Err()
	}
	return r.evidence.Analysis, r.analysisErr
}
func (r *cockpitReader) NodeContextHistory(context.Context, string, string) (api.NodeContextHistory, error) {
	r.historyCalls.Add(1)
	return r.nodeHistory, r.errorHistory
}

func cockpitFixture(t *testing.T) *cockpitReader {
	t.Helper()
	now := time.Now().UTC().Add(-time.Second)
	value := uint64(100 << 20)
	zero := uint64(0)
	one := uint64(1)
	o := nodecontext.Observation{NodeName: "node-a", NodeUID: "private-node-uid", ReportedAt: now, Availability: capability.Available,
		Evidence: capability.Envelope{Source: nodecontext.Source, Scope: capability.NodeScope, APIVersion: "v1alpha1", CapturedAt: now, ReceivedAt: now, Freshness: capability.Fresh, Completeness: capability.Partial, Stability: capability.ImplementationSpecific},
		Stats:    &nodecontext.Stats{StartedAt: now.Add(-time.Hour), Provenance: nodecontext.Unknown, Memory: &nodecontext.Memory{CapturedAt: now, UsageBytes: &value, AvailableBytes: &value, WorkingSetBytes: &value, RSSBytes: &value, PageFaults: &one, MajorPageFaults: &zero, PSI: &nodecontext.PSI{}}, Swap: &nodecontext.Swap{CapturedAt: now, UsageBytes: &zero, AvailableBytes: &value}},
		Context:  &nodecontext.KubernetesContext{CapturedAt: now, CapacityBytes: &value, AllocatableBytes: &value, MemoryPressure: "False", Hugepages: []nodecontext.Hugepage{{Resource: "hugepages-2Mi", CapacityBytes: &zero, AllocatableBytes: &zero}}}}
	for _, category := range []nodecontext.SystemCategory{nodecontext.Kubelet, nodecontext.Runtime, nodecontext.Misc, nodecontext.Pods} {
		o.Stats.SystemContainers = append(o.Stats.SystemContainers, nodecontext.SystemContainer{Category: category, StartedAt: now.Add(-time.Hour), Memory: o.Stats.Memory, Swap: o.Stats.Swap})
	}
	a, err := nodeanalysis.Analyse(nodeanalysis.Input{Now: now, NodeName: o.NodeName, NodeUID: o.NodeUID, Current: &o, SourceAvailability: capability.Available, Access: nodeanalysis.ClusterPods, Cgroup: nodeanalysis.CgroupFrame{NodeUID: o.NodeUID, CapturedAt: now, Coverage: nodeanalysis.Complete, Containers: []nodeanalysis.Container{{ID: "container", Namespace: "private-team", PodName: "private-pod", PodUID: "private-pod-uid", ContainerName: "app", WorkloadKind: "Deployment", WorkloadName: "private-workload", Charge: nodeanalysis.Charges{Total: 40 << 20, Anon: 30 << 20, Residual: 10 << 20}, CompositionConsistent: true, OOMKills: &one, OOMWindowStartedAt: now.Add(-5 * time.Second)}}}})
	if err != nil {
		t.Fatal(err)
	}
	return &cockpitReader{fakeSnapshotReader: &fakeSnapshotReader{}, evidence: api.NodeEvidence{Record: api.NodeContextRecord{NodeName: o.NodeName, NodeUID: o.NodeUID, ReceivedAt: now, Freshness: capability.Fresh, Report: &o, LastGood: &o}, Analysis: a}, nodeHistory: api.NodeContextHistory{NodeName: o.NodeName, Generation: "generation", ResetAt: now.Add(-time.Hour), WindowSeconds: 900, Completeness: capability.Partial, Series: []api.NodeContextHistorySeries{{NodeUID: o.NodeUID, Points: []api.NodeContextHistoryPoint{{ReceivedAt: now, Observation: o}}}}}}
}

func loadedCockpitModel(t *testing.T, width, height int) (appModel, *cockpitReader) {
	t.Helper()
	r := cockpitFixture(t)
	m := newModel(t.Context(), Options{AllNamespaces: true}, r, "test")
	m.width, m.height = width, height
	m.view = viewNodes
	m.loading = false
	m.data.ContainersLoaded = true
	m.data.Nodes = []api.NodeSnapshotStatus{{NodeName: "node-a", CapturedAt: time.Now()}}
	m.resizeViewports()
	cmd := m.ensureNodeTarget()
	if cmd == nil {
		t.Fatal("Node read not scheduled")
	}
	updated, _ := m.Update(cmd())
	m = updated.(appModel)
	if m.selectedNode.evidence == nil {
		t.Fatal("Node evidence did not arrive", m.selectedNode.err)
	}
	return m, r
}

func TestNodeStateRetainsHistoryButDropsPrivateSignalsOnFailure(t *testing.T) {
	m, r := loadedCockpitModel(t, 80, 24)
	if m.selectedNode.evidence.Analysis.Severity != nodeanalysis.Warning {
		t.Fatal("fixture requires Pod OOM warning")
	}
	r.analysisErr = errors.New("temporary failure")
	r.errorHistory = errors.New("temporary history failure")
	cmd := m.nodeRefreshCmd()
	updated, _ := m.Update(cmd())
	m = updated.(appModel)
	s := m.selectedNode
	if s.history == nil || s.historyErr == nil || s.evidence == nil || s.evidence.Analysis.Rankings != nil || s.evidence.Analysis.ObservedPodCharge != nil || s.evidence.Analysis.Severity != nodeanalysis.Normal {
		t.Fatal("transient failure lost Node history or retained private derived data", s)
	}
	r.analysisErr = &client.ReadError{Kind: client.ReadErrorForbidden}
	r.errorHistory = r.analysisErr
	cmd = m.nodeRefreshCmd()
	updated, _ = m.Update(cmd())
	m = updated.(appModel)
	if m.selectedNode.evidence != nil || m.selectedNode.history != nil {
		t.Fatal("revoked Node resources retained")
	}
	r.analysisErr = nil
	r.errorHistory = nil
	cmd = m.nodeRefreshCmd()
	updated, _ = m.Update(cmd())
	m = updated.(appModel)
	if m.selectedNode.evidence == nil || m.selectedNode.history == nil {
		t.Fatal("Node read did not recover")
	}
}

func TestNodePauseCancelsAndRejectsLateResponses(t *testing.T) {
	m, r := loadedCockpitModel(t, 80, 24)
	r.started = make(chan string, 2)
	r.cancelled = make(chan string, 2)
	cmd := m.nodeRefreshCmd()
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	select {
	case <-r.started:
	case <-time.After(time.Second):
		t.Fatal("Node read did not start")
	}
	updated, _ := m.Update(keyMessage("space"))
	m = updated.(appModel)
	select {
	case <-r.cancelled:
	case <-time.After(time.Second):
		t.Fatal("pause did not cancel Node read")
	}
	late := <-done
	previous := m.selectedNode.evidence
	updated, _ = m.Update(late)
	m = updated.(appModel)
	if !m.paused || m.selectedNode.inFlight || m.selectedNode.evidence != previous {
		t.Fatal("late response changed paused data")
	}
	if m.nodeRefreshCmd() != nil || m.ensureNodeTarget() != nil {
		t.Fatal("paused Node poll scheduled")
	}
	updated, _ = m.Update(tea.WindowSizeMsg{Width: 160, Height: 35})
	m = updated.(appModel)
	if m.selectedNode.inFlight {
		t.Fatal("resize polled while paused")
	}
	r.started = nil
	r.cancelled = nil
	updated, cmd = m.Update(keyMessage("space"))
	m = updated.(appModel)
	if m.paused || cmd == nil || !m.selectedNode.inFlight {
		t.Fatal("resume did not schedule selected Node")
	}
	updated, _ = m.Update(cmd())
	m = updated.(appModel)
	if m.selectedNode.err != nil {
		t.Fatal(m.selectedNode.err)
	}
}

func TestNodeSelectionAndRankRejectPreviousGeneration(t *testing.T) {
	r := cockpitFixture(t)
	var s selectedNode
	s.selectName("node-a")
	old, _ := s.start()
	if _, ok := s.start(); ok {
		t.Fatal("overlapping Node requests")
	}
	s.selectName("node-b")
	next, _ := s.start()
	if s.complete(nodeMsg{request: old, evidence: &r.evidence}, time.Now()) {
		t.Fatal("previous Node response accepted")
	}
	if !s.complete(nodeMsg{request: next}, time.Now()) {
		t.Fatal("new Node response discarded")
	}
	m, _ := loadedCockpitModel(t, 80, 24)
	seen := map[nodeanalysis.Metric]bool{}
	for i := 0; i < 7; i++ {
		seen[m.selectedNode.rank] = true
		cmd := m.cycleNodeRank()
		updated, _ := m.Update(cmd())
		m = updated.(appModel)
	}
	if len(seen) != 7 || m.selectedNode.rank != nodeanalysis.Total {
		t.Fatal("missing Node contributor ranking", seen)
	}
}

func TestNodeSecondaryDenialClearsClusterPodCacheAndLateFetch(t *testing.T) {
	m, r := loadedCockpitModel(t, 80, 24)
	m.data.Pods = []api.PodSnapshot{{Namespace: "secret", PodName: "cached"}}
	m.action.overwriteRequest = &actionRequest{kind: actionCapture, pods: m.data.Pods}
	m.action.pendingRequest = &actionRequest{kind: actionCapture, pods: m.data.Pods}
	generation := m.fetchGeneration
	denyCockpitContributors(t, r)
	cmd := m.nodeRefreshCmd()
	updated, _ := m.Update(cmd())
	m = updated.(appModel)
	if len(m.data.Pods) != 0 || m.selectedNode.evidence.Analysis.Rankings != nil {
		t.Fatal("secondary denial retained cluster contributors")
	}
	if m.action.overwriteRequest != nil || m.action.pendingRequest != nil {
		t.Fatal("secondary denial retained a cached Pod export")
	}
	updated, _ = m.Update(fetchMsg{generation: generation, data: snapshotData{Pods: []api.PodSnapshot{{PodName: "late-private"}}}})
	m = updated.(appModel)
	if len(m.data.Pods) != 0 {
		t.Fatal("late main fetch restored revoked data")
	}
}

func denyCockpitContributors(t *testing.T, r *cockpitReader) {
	t.Helper()
	var err error
	r.evidence.Analysis, err = nodeanalysis.Analyse(nodeanalysis.Input{Now: time.Now().UTC(), NodeName: r.evidence.Record.NodeName, NodeUID: r.evidence.Record.NodeUID, Current: r.evidence.Record.LastGood, SourceAvailability: capability.Available, Access: nodeanalysis.NodeOnly})
	if err != nil {
		t.Fatal(err)
	}
}

func TestNodeClusterDenialPreservesSeparatelyScopedPods(t *testing.T) {
	m, r := loadedCockpitModel(t, 80, 24)
	m.opts.AllNamespaces = false
	m.data.Pods = []api.PodSnapshot{{Namespace: "allowed-team", PodName: "allowed-pod"}}
	denyCockpitContributors(t, r)
	cmd := m.nodeRefreshCmd()
	updated, _ := m.Update(cmd())
	m = updated.(appModel)
	if len(m.data.Pods) != 1 || m.selectedNode.evidence.Analysis.Rankings != nil || m.selectedNode.evidence.Analysis.Severity != nodeanalysis.Normal {
		t.Fatal("Node denial changed independent namespace evidence or retained private analysis")
	}
}

func TestNodeSelectionSurvivesChargeReordering(t *testing.T) {
	m, _ := loadedCockpitModel(t, 80, 24)
	m.data.Nodes = append(m.data.Nodes, api.NodeSnapshotStatus{NodeName: "node-b"})
	m.reconcileCurrentViewport("")
	m.viewports[viewNodes].selected = 1
	m.selectedNode.selectName("node-b")
	next := snapshotData{Nodes: m.data.Nodes, ContainersLoaded: true, Pods: []api.PodSnapshot{{NodeName: "node-b"}}}
	next.Pods[0].Memory.TotalBytes = 100
	updated, _ := m.Update(fetchMsg{generation: m.fetchGeneration, data: next})
	m = updated.(appModel)
	ref, ok := m.currentEntityRef()
	if !ok || ref.nodeName != "node-b" || m.selectedNode.name != "node-b" || m.viewports[viewNodes].selected != 0 {
		t.Fatal("refresh moved the selected Node", ref)
	}
}
