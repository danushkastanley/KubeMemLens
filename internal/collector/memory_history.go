package collector

import (
	"context"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/memoryhistory"
)

type localMemoryHistory struct{ store *Store }
type retainedMemorySample struct {
	at    time.Time
	bytes uint64
}

// MemoryHistory reads existing bounded retention. It adds no store, collector
// work, metrics labels or background polling.
func (s *Store) MemoryHistory() memoryhistory.Provider { return &localMemoryHistory{store: s} }

func (p *localMemoryHistory) Query(ctx context.Context, s memoryhistory.Selection, q memoryhistory.Query) (memoryhistory.Report, error) {
	r, err := memoryhistory.NewReport(s, q, time.Now().UTC())
	if err != nil {
		return memoryhistory.Report{}, err
	}
	if q.Source != memoryhistory.Local {
		return unsupportedLocal(r), nil
	}
	if s.Request.Scope == memoryhistory.Container {
		return unsupportedLocal(r), nil
	}
	if s.Request.Scope == memoryhistory.Node {
		return p.nodeHistory(ctx, r)
	}
	if q.Metric != memoryhistory.Charge {
		return unsupportedLocal(r), nil
	}
	original := r.Series
	r.Series = nil
	seen := map[string]bool{}
	partial := false
	for _, series := range original {
		if ctx.Err() != nil {
			return memoryhistory.Report{}, ctx.Err()
		}
		target := series.Target
		key := historyKey(target.Namespace, target.Pod, target.PodUID, target.Node)
		if seen[key] {
			continue
		}
		seen[key] = true
		if target.PodCreatedAt.IsZero() {
			return memoryhistory.Report{}, memoryhistory.ErrChanged
		}
		target.Container, target.ContainerID = "", ""
		target.StartedAt = target.PodCreatedAt
		series.Target, series.Origin, series.SampleClock = target, "cgroup-v2", "collector-capture"
		samples, lost := p.store.retainedPodMemory(target, q, r.ReceivedAt)
		partial = partial || lost
		series.FreshFor = min(p.store.history.opts.ContinuityGap, memoryhistory.SampleMaxAge)
		fillRetained(&series, samples, series.FreshFor)
		r.Series = append(r.Series, series)
	}
	memoryhistory.Summarise(&r, partial)
	if r.State == memoryhistory.Missing {
		r.Reason = "local-history-unreported"
	}
	return r, nil
}

func unsupportedLocal(r memoryhistory.Report) memoryhistory.Report {
	r.State, r.Reason = memoryhistory.Unsupported, "local-metric-not-retained"
	r.Series = nil
	return r
}

func (s *Store) retainedPodMemory(target memoryhistory.Target, q memoryhistory.Query, now time.Time) ([]retainedMemorySample, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.history.prune(now)
	key := historyKey(target.Namespace, target.Pod, target.PodUID, target.Node)
	series := s.history.series[key]
	if series == nil {
		return nil, true
	}
	samples := make([]retainedMemorySample, 0, len(series.Points))
	for _, point := range series.Points {
		if !point.CapturedAt.Before(target.PodCreatedAt) {
			samples = append(samples, retainedMemorySample{point.CapturedAt, point.TotalBytes})
		}
	}
	coverage := s.history.coverage[key]
	lost := coverage == nil || s.history.resetAt.After(q.Start)
	if coverage != nil {
		lost = lost || (coverage.lastLossAt.After(q.Start) && !coverage.lastLossAt.After(q.End))
	}
	return samples, lost
}

// Carry only across the source's accepted continuity interval. Larger gaps stay
// missing; a stale retained value is never silently plotted as a fresh sample.
func fillRetained(series *memoryhistory.Series, samples []retainedMemorySample, continuity time.Duration) {
	index := 0
	for i, point := range series.Points {
		for index+1 < len(samples) && !samples[index+1].at.After(point.At) {
			index++
		}
		if len(samples) == 0 || samples[index].at.After(point.At) || point.At.Sub(samples[index].at) > continuity {
			continue
		}
		value := samples[index].bytes
		series.Points[i] = memoryhistory.Point{At: point.At, SampledAt: samples[index].at, Bytes: &value, State: memoryhistory.Fresh}
	}
}
