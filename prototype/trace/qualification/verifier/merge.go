package verifier

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
	queue          frameHeap
	cpus           map[uint32]cpuCursor
	aggregate      *Aggregate
	frontier       uint64
	failed, closed bool
}

func NewMerge(cpus []uint32) (*Merge, error) {
	if len(cpus) < 1 || len(cpus) > 64 {
		return nil, ErrObservation
	}
	m := &Merge{cpus: make(map[uint32]cpuCursor), aggregate: New()}
	for index, cpu := range cpus {
		if cpu > 4095 || (index > 0 && cpu <= cpus[index-1]) {
			return nil, ErrObservation
		}
		m.cpus[cpu] = cpuCursor{}
	}
	return m, nil
}

func (m *Merge) invalidate() error { m.failed = true; m.aggregate.failed = true; return ErrObservation }

func (m *Merge) Push(frame Frame) error {
	prior, exists := m.cpus[frame.CPU]
	e := frame.Event
	if m.failed || m.closed || !exists || frame.Sequence != prior.sequence+1 ||
		e.Time == 0 || e.Time < prior.stamp || e.Time <= m.frontier || e.TID == 0 ||
		e.Kind < CheckEnter || e.Kind > CheckReturn || len(m.queue) >= 4096 {
		return m.invalidate()
	}
	m.cpus[frame.CPU] = cpuCursor{e.Time, frame.Sequence}
	heap.Push(&m.queue, frame)
	return nil
}

func (m *Merge) Snapshot(cutoff uint64) (Totals, error) {
	if m.failed || m.closed || cutoff == 0 || cutoff < m.frontier {
		return Totals{}, m.invalidate()
	}
	for len(m.queue) > 0 && m.queue[0].Event.Time <= cutoff {
		frame := heap.Pop(&m.queue).(Frame)
		if m.aggregate.Observe(frame.Event) != nil {
			return Totals{}, m.invalidate()
		}
	}
	m.frontier = cutoff
	if m.aggregate.Advance(cutoff) != nil {
		return Totals{}, m.invalidate()
	}
	return m.aggregate.Snapshot()
}

func (m *Merge) Finish(cutoff uint64) (Totals, error) {
	if _, err := m.Snapshot(cutoff); err != nil {
		return Totals{}, err
	}
	m.closed = true
	return m.aggregate.Finish()
}
