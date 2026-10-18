package memorytopology

import (
	"encoding/json"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/capability"
)

// Failure reports an acquisition failure without synthesising source values or
// replacing their original capture timestamps with the failure time.
func Failure(name, uid string, at time.Time) Observation {
	return Observation{SchemaVersion: SchemaVersion, NodeName: name, NodeUID: uid, ReportedAt: at,
		NUMA: missing[NUMANode](NUMASysfs), Pools: missing[HugePool](HugeTLBSysfs), Cgroup: missing[HugeCgroup](HugeTLBCgroup)}
}

func missing[T any](source Source) Section[T] {
	return Section[T]{Source: source, Availability: capability.Unavailable, Completeness: capability.Partial, Reason: SourceFailed}
}

// Retain keeps the most recent successful acquisition for each source. The
// result is historical evidence, validated with maxAge=0 on subsequent reads.
// Disabled, forbidden and unsupported sources revoke that source's old data.
func Retain(previous *Observation, current Observation, now time.Time) (Observation, error) {
	if err := current.Validate(now, FreshFor); err != nil {
		return Observation{}, err
	}
	if previous != nil {
		if err := previous.Validate(now, 0); err != nil {
			return Observation{}, err
		}
		if previous.NodeName != current.NodeName || previous.NodeUID != current.NodeUID {
			return Observation{}, ErrInvalid
		}
		current.NUMA = retainSection(previous.NUMA, current.NUMA)
		current.Pools = retainSection(previous.Pools, current.Pools)
		current.Cgroup = retainSection(previous.Cgroup, current.Cgroup)
	}
	// Round-trip also enforces the aggregate byte budget after combining sources.
	data, err := json.Marshal(current)
	if err != nil {
		return Observation{}, err
	}
	var result Observation
	if err := json.Unmarshal(data, &result); err != nil {
		return Observation{}, err
	}
	return result, result.Validate(now, 0)
}

func retainSection[T any](previous, current Section[T]) Section[T] {
	if (current.Availability == capability.Unavailable || current.Availability == capability.Unreported) && previous.Availability == capability.Available {
		return previous
	}
	return current
}

// CapturedBefore rejects source-clock replay against retained source evidence.
func (o Observation) CapturedBefore(previous Observation) bool {
	return before(o.NUMA, previous.NUMA) || before(o.Pools, previous.Pools) || before(o.Cgroup, previous.Cgroup)
}

func before[T any](current, previous Section[T]) bool {
	return current.Availability == capability.Available && current.CapturedAt.Before(previous.CapturedAt)
}
