package tui

import (
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/capability"
	"github.com/danushkastanley/kube-memlens/internal/client"
	"github.com/danushkastanley/kube-memlens/internal/nodeanalysis"
)

type selectedNode struct {
	name             string
	rank             nodeanalysis.Metric
	generation       uint64
	inFlight         bool
	evidence         *api.NodeEvidence
	history          *api.NodeContextHistory
	err              error
	historyErr       error
	updatedAt        time.Time
	historyUpdatedAt time.Time
}

type nodeRequest struct {
	name       string
	rank       nodeanalysis.Metric
	generation uint64
}
type nodeMsg struct {
	request    nodeRequest
	evidence   *api.NodeEvidence
	history    *api.NodeContextHistory
	err        error
	historyErr error
}

func (s *selectedNode) selectName(name string) {
	if name == s.name {
		return
	}
	generation := s.generation + 1
	*s = selectedNode{name: name, rank: nodeanalysis.Total, generation: generation}
}

func (s *selectedNode) start() (nodeRequest, bool) {
	if s.name == "" || s.inFlight {
		return nodeRequest{}, false
	}
	if s.rank == "" {
		s.rank = nodeanalysis.Total
	}
	s.generation++
	s.inFlight = true
	return nodeRequest{name: s.name, rank: s.rank, generation: s.generation}, true
}

func (s *selectedNode) suspend() { s.generation++; s.inFlight = false }

func (s *selectedNode) complete(msg nodeMsg, now time.Time) bool {
	if msg.request.name != s.name || msg.request.generation != s.generation || !s.inFlight {
		return false
	}
	s.inFlight = false
	s.err = msg.err
	s.historyErr = msg.historyErr
	if msg.err == nil {
		s.evidence = msg.evidence
		s.updatedAt = now
	} else if client.IsForbidden(msg.err) || client.IsNotFound(msg.err) {
		s.evidence = nil
		s.updatedAt = time.Time{}
	} else {
		s.dropContributors(now)
	}
	if msg.historyErr == nil {
		s.history = msg.history
		s.historyUpdatedAt = now
	} else if client.IsForbidden(msg.historyErr) || client.IsNotFound(msg.historyErr) {
		s.history = nil
		s.historyUpdatedAt = time.Time{}
	}
	return true
}

func (s *selectedNode) dropContributors(now time.Time) {
	if s.evidence == nil {
		return
	}
	e := *s.evidence
	availability := capability.Unreported
	if e.Record.Report != nil {
		availability = e.Record.Report.Availability
	}
	a, err := nodeanalysis.Analyse(nodeanalysis.Input{Now: now, NodeName: e.Record.NodeName, NodeUID: e.Record.NodeUID, Current: e.Record.LastGood, SourceAvailability: availability, Access: nodeanalysis.NodeOnly})
	if err != nil {
		s.evidence = nil
		return
	}
	e.Analysis = a
	e.Record.Freshness = capability.Stale
	e.Analysis.Confidence = nodeanalysis.Low
	s.evidence = &e
}
