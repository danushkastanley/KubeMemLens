package incident

import (
	"slices"

	"github.com/danushkastanley/kube-memlens/internal/memorytopology"
	"github.com/danushkastanley/kube-memlens/internal/nodecontext"
)

func topologyIDs(b TopologyBundle) []uint16 {
	seen := map[uint16]bool{}
	for _, r := range []*memorytopology.Report{b.Current, b.LastGood} {
		if r == nil {
			continue
		}
		for _, n := range r.Observation.NUMA.Items {
			seen[n.ID] = true
		}
		for _, g := range r.Observation.Cgroup.Items {
			for _, n := range g.NUMA {
				seen[n.ID] = true
			}
		}
	}
	ids := make([]uint16, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}
func redactTopology(b *TopologyBundle) error {
	ids := topologyIDs(*b)
	aliases := map[uint16]uint16{}
	for i, id := range ids {
		aliases[id] = uint16(i)
	}
	redactNode(&b.Node)
	b.Redacted = true
	b.Caveats = []string{topologyAliasCaveat}
	e := &b.Node.Evidence
	e.Record.NodeName = "node-1"
	e.Analysis.NodeName = "node-1"
	for _, o := range []*nodecontext.Observation{e.Record.Report, e.Record.LastGood} {
		if o != nil {
			o.NodeName = "node-1"
		}
	}
	if b.Node.History != nil {
		b.Node.History.NodeName = "node-1"
		for i := range b.Node.History.Series {
			for j := range b.Node.History.Series[i].Points {
				b.Node.History.Series[i].Points[j].Observation.NodeName = "node-1"
			}
		}
	}
	for _, target := range []**memorytopology.Report{&b.Current, &b.LastGood} {
		r := *target
		if r == nil {
			continue
		}
		o := r.Observation
		o.NodeName = "node-1"
		o.NodeUID = e.Record.NodeUID
		for i := range o.NUMA.Items {
			o.NUMA.Items[i].ID = aliases[o.NUMA.Items[i].ID]
		}
		for i := range o.Cgroup.Items {
			for j := range o.Cgroup.Items[i].NUMA {
				o.Cgroup.Items[i].NUMA[j].ID = aliases[o.Cgroup.Items[i].NUMA[j].ID]
			}
		}
		updated, err := memorytopology.Analyse(o, r.ObservedAt)
		if err != nil {
			return err
		}
		*target = &updated
	}
	return nil
}
