package model

import "errors"

// Zswap comes from the zswap and zswapped keys of memory.stat. PoolBytes is
// the memory the compressed pool uses; SwappedBytes is the application memory
// held in it. Known only when the kernel reports both keys.
type Zswap struct {
	Known        bool   `json:"known,omitempty"`
	PoolBytes    uint64 `json:"poolBytes,omitempty"`
	SwappedBytes uint64 `json:"swappedBytes,omitempty"`
}

// OOMGroupKill is the oom_group_kill key of memory.events (Events) and of
// memory.events.local (LocalEvents). Each is known only when its file has the
// key: older kernels do not report it. The delta fields are set by the
// collector only.
type OOMGroupKill struct {
	Known            bool   `json:"known,omitempty"`
	Events           uint64 `json:"events,omitempty"`
	LocalKnown       bool   `json:"localKnown,omitempty"`
	LocalEvents      uint64 `json:"localEvents,omitempty"`
	DeltasKnown      bool   `json:"deltasKnown,omitempty"`
	EventsDelta      uint64 `json:"eventsDelta,omitempty"`
	LocalDeltasKnown bool   `json:"localDeltasKnown,omitempty"`
	LocalEventsDelta uint64 `json:"localEventsDelta,omitempty"`
}

var (
	errInvalidZswap        = errors.New("invalid zswap observation")
	errInvalidOOMGroupKill = errors.New("invalid oom_group_kill observation")
)

func (z Zswap) Validate() error {
	if !z.Known && z != (Zswap{}) {
		return errInvalidZswap
	}
	return nil
}

func (g OOMGroupKill) Validate() error {
	if !validGroupKillCounter(g.Known, g.Events, g.DeltasKnown, g.EventsDelta) ||
		!validGroupKillCounter(g.LocalKnown, g.LocalEvents, g.LocalDeltasKnown, g.LocalEventsDelta) {
		return errInvalidOOMGroupKill
	}
	return nil
}

func validGroupKillCounter(known bool, events uint64, deltaKnown bool, delta uint64) bool {
	if !known {
		return events == 0 && !deltaKnown && delta == 0
	}
	if !deltaKnown {
		return delta == 0
	}
	return delta <= events
}

// withOOMGroupKillDeltas applies the first-sample rule of WithEventDeltas:
// each delta is known when its counter is, and is zero without a previous
// sample of the same container instance. Every delta is overwritten.
func withOOMGroupKillDeltas(current, previous OOMGroupKill, hasPrevious bool) OOMGroupKill {
	current.DeltasKnown, current.LocalDeltasKnown = current.Known, current.LocalKnown
	current.EventsDelta, current.LocalEventsDelta = 0, 0
	if !hasPrevious {
		return current
	}
	if current.Known && previous.Known {
		current.EventsDelta = counterDelta(current.Events, previous.Events)
	}
	if current.LocalKnown && previous.LocalKnown {
		current.LocalEventsDelta = counterDelta(current.LocalEvents, previous.LocalEvents)
	}
	return current
}

// addZswap and addOOMGroupKill follow the rule AddMemory uses for the swap
// fields (SwapCurrentKnown: a.SwapCurrentKnown || b.SwapCurrentKnown, with the
// bytes summed): a value is known in the aggregate when any member reports it,
// and members that do not report it add zero.
func addZswap(a, b Zswap) Zswap {
	return Zswap{
		Known:        a.Known || b.Known,
		PoolBytes:    a.PoolBytes + b.PoolBytes,
		SwappedBytes: a.SwappedBytes + b.SwappedBytes,
	}
}

func addOOMGroupKill(a, b OOMGroupKill) OOMGroupKill {
	return OOMGroupKill{
		Known:            a.Known || b.Known,
		Events:           a.Events + b.Events,
		LocalKnown:       a.LocalKnown || b.LocalKnown,
		LocalEvents:      a.LocalEvents + b.LocalEvents,
		DeltasKnown:      a.DeltasKnown || b.DeltasKnown,
		EventsDelta:      a.EventsDelta + b.EventsDelta,
		LocalDeltasKnown: a.LocalDeltasKnown || b.LocalDeltasKnown,
		LocalEventsDelta: a.LocalEventsDelta + b.LocalEventsDelta,
	}
}
