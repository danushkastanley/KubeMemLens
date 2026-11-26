package model

import "errors"

// PodCgroupMemory is the Pod's own cgroup: the parent of its containers and of
// its sandbox. CurrentBytes therefore includes the sandbox and is not the sum
// of the container totals. Only the Pod cgroup's local events are carried: its
// hierarchical counters sum the subtree, which the containers already report.
//
// Each value has its own Known flag. An unknown value is zero and must be read
// as unknown, never as a measured zero. The Local*Delta fields are set by the
// collector only; an agent-supplied delta is overwritten on ingest.
type PodCgroupMemory struct {
	CurrentBytes uint64 `json:"currentBytes,omitempty"`
	CurrentKnown bool   `json:"currentKnown,omitempty"`
	MaxBytes     uint64 `json:"maxBytes,omitempty"`
	MaxKnown     bool   `json:"maxKnown,omitempty"`
	MaxUnlimited bool   `json:"maxUnlimited,omitempty"`

	LocalEventsKnown   bool   `json:"localEventsKnown,omitempty"`
	LocalOOMEvents     uint64 `json:"localOOMEvents,omitempty"`
	LocalOOMKillEvents uint64 `json:"localOOMKillEvents,omitempty"`
	LocalMaxEvents     uint64 `json:"localMaxEvents,omitempty"`

	LocalEventDeltasKnown   bool   `json:"localEventDeltasKnown,omitempty"`
	LocalOOMEventsDelta     uint64 `json:"localOOMEventsDelta,omitempty"`
	LocalOOMKillEventsDelta uint64 `json:"localOOMKillEventsDelta,omitempty"`
	LocalMaxEventsDelta     uint64 `json:"localMaxEventsDelta,omitempty"`
}

var errInvalidPodCgroup = errors.New("invalid Pod cgroup observation")

// Validate rejects values that claim a number for something unknown. The
// counters are fixed-size numbers and need no other bound.
func (p PodCgroupMemory) Validate() error {
	if !p.CurrentKnown && p.CurrentBytes != 0 {
		return errInvalidPodCgroup
	}
	if !p.MaxKnown && (p.MaxBytes != 0 || p.MaxUnlimited) {
		return errInvalidPodCgroup
	}
	if p.MaxUnlimited && p.MaxBytes != 0 {
		return errInvalidPodCgroup
	}
	if !p.LocalEventsKnown && (p.LocalOOMEvents != 0 || p.LocalOOMKillEvents != 0 || p.LocalMaxEvents != 0 || p.LocalEventDeltasKnown) {
		return errInvalidPodCgroup
	}
	if !p.LocalEventDeltasKnown && (p.LocalOOMEventsDelta != 0 || p.LocalOOMKillEventsDelta != 0 || p.LocalMaxEventsDelta != 0) {
		return errInvalidPodCgroup
	}
	if p.LocalOOMEventsDelta > p.LocalOOMEvents || p.LocalOOMKillEventsDelta > p.LocalOOMKillEvents || p.LocalMaxEventsDelta > p.LocalMaxEvents {
		return errInvalidPodCgroup
	}
	return nil
}

// WithPodCgroupDeltas follows the first-sample rule of WithEventDeltas: the
// local deltas are known whenever the local counters are, and without a
// previous sample of the same Pod they are zero, so counters accumulated
// before the first observation are history, not recent events. Every delta is
// overwritten here. A counter that went down (a recreated cgroup) gives a zero
// delta, as counterDelta does for container events.
func WithPodCgroupDeltas(current, previous PodCgroupMemory, hasPrevious bool) PodCgroupMemory {
	current.LocalEventDeltasKnown = current.LocalEventsKnown
	current.LocalOOMEventsDelta, current.LocalOOMKillEventsDelta, current.LocalMaxEventsDelta = 0, 0, 0
	if !hasPrevious || !current.LocalEventsKnown || !previous.LocalEventsKnown {
		return current
	}
	current.LocalOOMEventsDelta = counterDelta(current.LocalOOMEvents, previous.LocalOOMEvents)
	current.LocalOOMKillEventsDelta = counterDelta(current.LocalOOMKillEvents, previous.LocalOOMKillEvents)
	current.LocalMaxEventsDelta = counterDelta(current.LocalMaxEvents, previous.LocalMaxEvents)
	return current
}

// KeepFirstPodCgroup builds a Pod's value from its containers. Ingest requires
// every container of one Pod that carries the value to carry the same one, so
// the first non-zero value stands for the Pod. Containers without it add
// nothing.
func KeepFirstPodCgroup(pod, container PodCgroupMemory) PodCgroupMemory {
	if pod != (PodCgroupMemory{}) {
		return pod
	}
	return container
}
