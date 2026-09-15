package traceframe

import (
	"github.com/danushkastanley/kube-memlens/internal/trace"
	"github.com/danushkastanley/kube-memlens/internal/traceaggregate"
	"math/bits"
)

type wireTotal struct {
	Value      *uint64 `json:"value"`
	Unreported bool    `json:"unreported"`
	Overflow   bool    `json:"overflow"`
}
type wireFileOperations struct {
	Operations uint64    `json:"operations"`
	Requested  wireTotal `json:"totalRequested"`
	Completed  wireTotal `json:"totalCompleted"`
}
type wireCacheOperations struct {
	Operations uint64    `json:"operations"`
	Pages      wireTotal `json:"totalPages"`
}
type wireFileAggregates struct {
	Observations uint64             `json:"observations"`
	Reads        wireFileOperations `json:"reads"`
	Writes       wireFileOperations `json:"writes"`
}
type wireCacheAggregates struct {
	Observations uint64              `json:"observations"`
	Additions    wireCacheOperations `json:"additions"`
	Removals     wireCacheOperations `json:"removals"`
}

func totalWire(t traceaggregate.Total) wireTotal { return wireTotal{t.Value, t.Unreported, t.Overflow} }
func filesWire(f traceaggregate.FileOperations) wireFileOperations {
	return wireFileOperations{f.Operations, totalWire(f.RequestedBytes), totalWire(f.CompletedBytes)}
}
func cacheWire(c traceaggregate.CacheOperations) wireCacheOperations {
	return wireCacheOperations{c.Operations, totalWire(c.Pages)}
}

func setAggregates(w *wireSummary, s *traceaggregate.Summary) error {
	if w.Termination == trace.AuthorisationLost {
		if s != nil || len(w.Correlation) != 0 || len(w.OOMCorrelation) != 0 || len(w.KubernetesContext) != 0 || w.ObservationStartedAt != nil || w.ObservationEndedAt != nil || w.EngineCounts.Produced != nil || w.EngineCounts.Sampled != nil || w.EngineCounts.Lost != nil || w.EngineCounts.Rejected != nil {
			return ErrInvalid
		}
		return nil
	}
	if s == nil {
		if w.Termination == trace.Expired {
			return ErrInvalid
		}
		return nil
	}
	switch s.Kind {
	case trace.Files:
		w.FileAggregates = &wireFileAggregates{s.Observations, filesWire(s.Reads), filesWire(s.Writes)}
	case trace.Cache:
		w.CacheAggregates = &wireCacheAggregates{s.Observations, cacheWire(s.Additions), cacheWire(s.Removals)}
	case trace.OOM:
		w.OOMAggregates = &wireOOMAggregates{s.Observations, s.OOM.Cgroup, s.OOM.Global, s.OOM.Unknown, s.OOM.MissingProcessContext}
	default:
		return ErrInvalid
	}
	return validateAggregates(*w)
}

func validTotal(t wireTotal, operations uint64) bool {
	if (t.Value == nil) != (t.Unreported || t.Overflow) {
		return false
	}
	return operations != 0 || (t.Value != nil && *t.Value == 0 && !t.Unreported && !t.Overflow)
}

func aggregateCount(s wireSummary) uint64 {
	if s.FileAggregates != nil {
		return s.FileAggregates.Observations
	}
	if s.CacheAggregates != nil {
		return s.CacheAggregates.Observations
	}
	if s.OOMAggregates != nil {
		return s.OOMAggregates.Observations
	}
	return 0
}

func validateAggregates(s wireSummary) error {
	if (s.FileAggregates != nil && s.CacheAggregates != nil) || validateOOMAggregates(s) != nil {
		return ErrInvalid
	}
	if s.FileAggregates == nil && s.CacheAggregates == nil && s.OOMAggregates == nil {
		if s.Termination == trace.Expired {
			return ErrInvalid
		}
		return nil
	}
	if s.Termination == trace.AuthorisationLost || !s.Incomplete {
		return ErrInvalid
	}
	count := aggregateCount(s)
	if count > 100000 || s.WrittenEvents > count {
		return ErrInvalid
	}
	lower, carry := bits.Add64(count, s.RejectedEvents, 0)
	if carry != 0 {
		return ErrInvalid
	}
	for _, value := range []*uint64{s.EngineCounts.Sampled, s.EngineCounts.Lost, s.EngineCounts.Rejected} {
		if value == nil {
			continue
		}
		lower, carry = bits.Add64(lower, *value, 0)
		if carry != 0 {
			return ErrInvalid
		}
	}
	if s.EngineCounts.Produced != nil && lower > *s.EngineCounts.Produced {
		return ErrInvalid
	}
	if f := s.FileAggregates; f != nil {
		if f.Reads.Operations > count || f.Writes.Operations != count-f.Reads.Operations {
			return ErrInvalid
		}
		for _, row := range []wireFileOperations{f.Reads, f.Writes} {
			if !validTotal(row.Requested, row.Operations) || !validTotal(row.Completed, row.Operations) || (row.Requested.Value != nil && row.Completed.Value != nil && *row.Completed.Value > *row.Requested.Value) {
				return ErrInvalid
			}
		}
	}
	if c := s.CacheAggregates; c != nil {
		if c.Additions.Operations > count || c.Removals.Operations != count-c.Additions.Operations {
			return ErrInvalid
		}
		for _, row := range []wireCacheOperations{c.Additions, c.Removals} {
			if !validTotal(row.Pages, row.Operations) || row.Pages.Unreported || (row.Pages.Value != nil && *row.Pages.Value < row.Operations) {
				return ErrInvalid
			}
		}
	}
	return nil
}

func validateAggregateEvent(e wireEvent) error {
	f := e.File
	if f == nil || e.Cache != nil || e.OOM != nil || f.Path == nil || *f.Path == "" || (f.Operation != trace.FileRead && f.Operation != trace.FileWrite) || (f.RequestedBytes != nil && f.CompletedBytes != nil && *f.CompletedBytes > *f.RequestedBytes) {
		return ErrInvalid
	}
	return nil
}

func totalDomain(t wireTotal) traceaggregate.Total {
	return traceaggregate.Total{Value: t.Value, Unreported: t.Unreported, Overflow: t.Overflow}
}
func fileDomain(f wireFileOperations) traceaggregate.FileOperations {
	return traceaggregate.FileOperations{Operations: f.Operations, RequestedBytes: totalDomain(f.Requested), CompletedBytes: totalDomain(f.Completed)}
}
func cacheDomain(c wireCacheOperations) traceaggregate.CacheOperations {
	return traceaggregate.CacheOperations{Operations: c.Operations, Pages: totalDomain(c.Pages)}
}
func domainAggregates(s wireSummary) *traceaggregate.Summary {
	if s.OOMAggregates != nil {
		return oomDomain(s.OOMAggregates)
	}
	if f := s.FileAggregates; f != nil {
		return &traceaggregate.Summary{Kind: trace.Files, Observations: f.Observations, Reads: fileDomain(f.Reads), Writes: fileDomain(f.Writes)}
	}
	if c := s.CacheAggregates; c != nil {
		return &traceaggregate.Summary{Kind: trace.Cache, Observations: c.Observations, Additions: cacheDomain(c.Additions), Removals: cacheDomain(c.Removals)}
	}
	return nil
}
