package nodestats

import (
	"context"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/memorytopology"
)

// TopologySource is supplied only by the explicitly enabled local source profile.
type TopologySource interface {
	Read(context.Context, string, string) (memorytopology.Observation, error)
}

func (s *Source) topology(ctx context.Context, name, uid string) *memorytopology.Observation {
	if s.opts.Topology == nil {
		return nil
	}
	observation, err := s.opts.Topology.Read(ctx, name, uid)
	now := s.opts.Now().UTC()
	if err != nil || observation.Validate(now, memorytopology.FreshFor) != nil || observation.NodeName != name || observation.NodeUID != uid {
		observation = memorytopology.Failure(name, uid, now)
	}
	return &observation
}

func (s *Sample) stampTopology(at time.Time) {
	if s.Topology != nil {
		s.Topology.ReportedAt = at
	}
}
