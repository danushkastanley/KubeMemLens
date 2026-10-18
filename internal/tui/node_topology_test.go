package tui

import (
	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/client"
	"github.com/danushkastanley/kube-memlens/internal/memorytopology"
	"testing"
	"time"
)

func TestTopologySelectionGenerationAndRevocation(t *testing.T) {
	now := time.Now().UTC()
	report, err := memorytopology.Analyse(memorytopology.Failure("node-a", "uid-a", now), now)
	if err != nil {
		t.Fatal(err)
	}
	value := &api.NodeMemoryTopology{Current: &report}
	evidence := &api.NodeEvidence{Record: api.NodeContextRecord{NodeName: "node-a", NodeUID: "uid-a"}}
	var s selectedNode
	s.selectName("node-a")
	req, _ := s.start()
	if !s.complete(nodeMsg{request: req, evidence: evidence, topology: value}, now) || s.topology == nil {
		t.Fatal("matching topology rejected")
	}
	req, _ = s.start()
	s.complete(nodeMsg{request: req, evidence: evidence, topologyErr: &client.ReadError{Kind: client.ReadErrorForbidden}}, now)
	if s.topology != nil {
		t.Fatal("revocation retained topology")
	}
	req, _ = s.start()
	report.Observation.NodeUID = "replacement"
	s.complete(nodeMsg{request: req, evidence: evidence, topology: value}, now)
	if s.topology != nil {
		t.Fatal("mixed Node instances displayed")
	}
	req, _ = s.start()
	s.selectName("node-b")
	if s.complete(nodeMsg{request: req, evidence: evidence, topology: value}, now) || s.topology != nil {
		t.Fatal("late selection response restored topology")
	}
}
