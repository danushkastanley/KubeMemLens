package scheduler

import "container/heap"

type Frame struct {
	Event    Event
	CPU      uint32
	Sequence uint64
}
type frameHeap []Frame

func (h frameHeap) Len() int { return len(h) }
func (h frameHeap) Less(i, j int) bool {
	a, b := h[i], h[j]
	if a.Event.Time != b.Event.Time {
		return a.Event.Time < b.Event.Time
	}
	if a.CPU != b.CPU {
		return a.CPU < b.CPU
	}
	return a.Sequence < b.Sequence
}
func (h frameHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h *frameHeap) Push(v any)   { *h = append(*h, v.(Frame)) }
func (h *frameHeap) Pop() any {
	old := *h
	n := len(old)
	v := old[n-1]
	old[n-1] = Frame{}
	*h = old[:n-1]
	return v
}

type cpuCursor struct{ stamp, sequence uint64 }

type Merge struct {
	queue     frameHeap
	cpus      map[uint32]cpuCursor
	aggregate *Aggregate
	maximum   int
	frontier  uint64
	stamp     uint64
	touched   map[uint32]uint32
	failed    bool
}

// NewMerge fixes the CPU roster and buffer cap before capture. A later topology
// change, missing record or ambiguous cross-CPU tie invalidates the observation.
func NewMerge(cpus []uint32, maximum int) (*Merge, error) {
	if len(cpus) < 1 || len(cpus) > 64 || maximum < 1 || maximum > 131072 {
		return nil, ErrObservation
	}
	m := &Merge{cpus: make(map[uint32]cpuCursor), maximum: maximum, touched: make(map[uint32]uint32)}
	for _, cpu := range cpus {
		if cpu > 4095 {
			return nil, ErrObservation
		}
		if _, exists := m.cpus[cpu]; exists {
			return nil, ErrObservation
		}
		m.cpus[cpu] = cpuCursor{}
	}
	m.aggregate, _ = New(32768)
	return m, nil
}

func (m *Merge) invalidate() error { m.failed = true; m.aggregate.Lost(); return ErrObservation }

func (m *Merge) Push(frame Frame) error {
	if m.failed {
		return ErrObservation
	}
	previous, exists := m.cpus[frame.CPU]
	if !exists || !valid(frame.Event) || frame.Sequence != previous.sequence+1 || frame.Event.Time < previous.stamp || frame.Event.Time <= m.frontier || len(m.queue) >= m.maximum {
		return m.invalidate()
	}
	m.cpus[frame.CPU] = cpuCursor{frame.Event.Time, frame.Sequence}
	heap.Push(&m.queue, frame)
	return nil
}

func (m *Merge) observe(frame Frame) error {
	e := frame.Event
	if e.Time != m.stamp {
		clear(m.touched)
		m.stamp = e.Time
	}
	for _, pid := range []uint32{e.PID, e.PrevPID, e.NextPID} {
		if pid == 0 {
			continue
		}
		if cpu, exists := m.touched[pid]; exists && cpu != frame.CPU {
			return m.invalidate()
		}
		m.touched[pid] = frame.CPU
	}
	if m.aggregate.Observe(e) != nil {
		return m.invalidate()
	}
	return nil
}

// Advance publishes only records at or before a monotonic watermark. The native
// reader must drain every CPU first and enforce its fixed reorder allowance;
// any subsequent late arrival is a hard failure, not a silently retimed event.
func (m *Merge) Advance(frontier uint64) (Snapshot, error) {
	if m.failed || frontier < m.frontier {
		return Snapshot{}, m.invalidate()
	}
	for len(m.queue) > 0 && m.queue[0].Event.Time <= frontier {
		if m.observe(heap.Pop(&m.queue).(Frame)) != nil {
			return Snapshot{}, ErrObservation
		}
	}
	m.frontier = frontier
	return m.aggregate.Snapshot()
}

func (m *Merge) Lost() { _ = m.invalidate() }
