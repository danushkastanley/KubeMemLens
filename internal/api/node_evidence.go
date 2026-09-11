package api

import (
	"reflect"

	"github.com/danushkastanley/kube-memlens/internal/capability"
	"github.com/danushkastanley/kube-memlens/internal/nodeanalysis"
	"github.com/danushkastanley/kube-memlens/internal/nodecontext"
)

// NodeEvidence pairs an authorised analysis with its source record. Read
// adapters reject identity or sample changes between the independent requests.
type NodeEvidence struct {
	Record   NodeContextRecord     `json:"record"`
	Analysis nodeanalysis.Analysis `json:"analysis"`
}

// Consistent binds identities and raw facts across separately authorised reads.
func (e NodeEvidence) Consistent() bool {
	record, facts := e.Record, e.Analysis.Facts
	if record.NodeName == "" || record.NodeName != e.Analysis.NodeName || record.NodeUID == "" || record.NodeUID != e.Analysis.NodeUID {
		return false
	}
	availability := capability.Unreported
	if record.Report != nil {
		availability = record.Report.Availability
	}
	if facts.Availability != availability {
		return false
	}
	var memory *nodecontext.Memory
	var swap *nodecontext.Swap
	var context *nodecontext.KubernetesContext
	var systems []nodecontext.SystemContainer
	if record.LastGood != nil {
		context = record.LastGood.Context
		if record.LastGood.Stats != nil {
			memory, swap, systems = record.LastGood.Stats.Memory, record.LastGood.Stats.Swap, record.LastGood.Stats.SystemContainers
		}
	}
	return reflect.DeepEqual(memory, facts.Memory) && reflect.DeepEqual(swap, facts.Swap) && sameNodeContext(context, facts.Context) &&
		(len(systems) == 0 && len(facts.SystemContainers) == 0 || reflect.DeepEqual(systems, facts.SystemContainers))
}

func sameNodeContext(left, right *nodecontext.KubernetesContext) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	a, b := *left, *right
	// Both forms encode the same omitted optional list; the analysis clones it.
	if len(a.Hugepages) == 0 {
		a.Hugepages = nil
	}
	if len(b.Hugepages) == 0 {
		b.Hugepages = nil
	}
	return reflect.DeepEqual(a, b)
}
