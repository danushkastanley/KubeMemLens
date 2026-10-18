package collector

import (
	"encoding/json"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/capability"
	"github.com/danushkastanley/kube-memlens/internal/memorytopology"
	"github.com/danushkastanley/kube-memlens/internal/nodecontext"
)

// Two immutable bounded observations per owned Node; no additional index or
// per-point history. Node inventory replacement/removal also removes this data.
type topologyEntry struct{ current, good []byte }

func prepareTopologyEntry(node nodecontext.Observation, raw []byte, prior topologyEntry, now time.Time) (topologyEntry, error) {
	var current memorytopology.Observation
	if len(raw) == 0 {
		if len(prior.current) == 0 || node.Availability != capability.Unavailable {
			return topologyEntry{}, nil
		}
		current = memorytopology.Failure(node.NodeName, node.NodeUID, node.ReportedAt)
	} else if err := json.Unmarshal(raw, &current); err != nil {
		return topologyEntry{}, memorytopology.ErrInvalid
	}
	if current.NodeName != node.NodeName || current.NodeUID != node.NodeUID || !current.ReportedAt.Equal(node.ReportedAt) {
		return topologyEntry{}, memorytopology.ErrInvalid
	}
	if err := current.Validate(now, memorytopology.FreshFor); err != nil {
		return topologyEntry{}, err
	}
	var previous *memorytopology.Observation
	if len(prior.good) > 0 {
		previous = &memorytopology.Observation{}
		if err := json.Unmarshal(prior.good, previous); err != nil {
			return topologyEntry{}, err
		}
		if current.CapturedBefore(*previous) {
			return topologyEntry{}, ErrSnapshotOutOfOrder
		}
	}
	good, err := memorytopology.Retain(previous, current, now)
	if err != nil {
		return topologyEntry{}, err
	}
	currentBytes, err := json.Marshal(current)
	if err != nil {
		return topologyEntry{}, err
	}
	var goodBytes []byte
	if good.NUMA.Availability == capability.Available || good.Pools.Availability == capability.Available || good.Cgroup.Availability == capability.Available {
		goodBytes, err = json.Marshal(good)
		if err != nil {
			return topologyEntry{}, err
		}
	}
	return topologyEntry{current: currentBytes, good: goodBytes}, nil
}

// NodeTopology requires current Node inventory, in addition to the request's
// Node-only authorisation. Returned reports own all of their mutable values.
func (s *Store) NodeTopology(name string, now time.Time) (api.NodeMemoryTopology, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := api.NodeMemoryTopology{}
	uid := s.expectedNodeUIDs[name]
	if !s.nodeIdentityCurrentLocked(name, uid, now) {
		return result, false, ErrNodeIdentityUnavailable
	}
	entry := s.nodeContext.latest[name]
	if entry.uid != uid {
		return result, false, nil
	}
	for _, item := range []struct {
		data   []byte
		target **memorytopology.Report
	}{
		{entry.topology.current, &result.Current}, {entry.topology.good, &result.LastGood},
	} {
		if len(item.data) == 0 {
			continue
		}
		var observation memorytopology.Observation
		if err := json.Unmarshal(item.data, &observation); err != nil {
			return result, false, err
		}
		report, err := memorytopology.Analyse(observation, now)
		if err != nil {
			return result, false, err
		}
		*item.target = &report
	}
	return result, true, nil
}
