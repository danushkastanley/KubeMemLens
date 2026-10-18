package collector

import (
	"context"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/memoryhistory"
	"github.com/danushkastanley/kube-memlens/internal/nodecontext"
)

func (p *localMemoryHistory) nodeHistory(ctx context.Context, r memoryhistory.Report) (memoryhistory.Report, error) {
	if r.Query.Metric == memoryhistory.Charge {
		return unsupportedLocal(r), nil
	}
	p.store.mu.RLock()
	enabled := p.store.nodeContextEnabled
	p.store.mu.RUnlock()
	if !enabled {
		r.State, r.Reason = memoryhistory.Disabled, "local-node-source-disabled"
		r.Series = nil
		return r, nil
	}
	if ctx.Err() != nil {
		return memoryhistory.Report{}, ctx.Err()
	}
	samples, lost := p.store.retainedNodeMemory(r.Selection.Targets[0], r.Query, r.ReceivedAt)
	r.Series[0].Origin, r.Series[0].SampleClock = "kubelet-summary", "kubelet-sample"
	r.Series[0].FreshFor = min(nodecontext.StaleAfter, memoryhistory.SampleMaxAge)
	fillRetained(&r.Series[0], samples, r.Series[0].FreshFor)
	memoryhistory.Summarise(&r, lost)
	if r.State == memoryhistory.Missing {
		r.Reason = "local-history-unreported"
	}
	return r, nil
}

func (s *Store) retainedNodeMemory(target memoryhistory.Target, q memoryhistory.Query, now time.Time) ([]retainedMemorySample, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nodeContext.prune(now)
	series := s.nodeContext.history[target.NodeUID]
	if series == nil || series.name != target.Node {
		return nil, true
	}
	samples := make([]retainedMemorySample, 0, len(series.points))
	for _, point := range series.points {
		o := decodeNodeObservation(point.data)
		if o.Stats == nil || o.Stats.Memory == nil {
			continue
		}
		memory := o.Stats.Memory
		value := memory.WorkingSetBytes
		if q.Metric == memoryhistory.RSS {
			value = memory.RSSBytes
		}
		if value != nil && !memory.CapturedAt.Before(target.StartedAt) {
			samples = append(samples, retainedMemorySample{memory.CapturedAt, *value})
		}
	}
	lost := s.startedAt.After(q.Start) || (s.nodeContext.lossAt.After(q.Start) && !s.nodeContext.lossAt.After(q.End))
	return samples, lost
}
