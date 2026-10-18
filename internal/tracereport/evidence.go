package tracereport

import (
	"github.com/danushkastanley/kube-memlens/internal/trace"
	"github.com/danushkastanley/kube-memlens/internal/traceaggregate"
	"github.com/danushkastanley/kube-memlens/internal/traceframe"
)

func summaryFields(s traceframe.Summary) any {
	result := map[string]any{"sessionEndedAt": s.SessionEndedAt, "observationStartedAt": s.ObservationStartedAt, "observationEndedAt": s.ObservationEndedAt,
		"termination": s.Termination, "incomplete": s.Incomplete, "engineCounts": map[string]any{"produced": s.EngineCounts.Produced, "sampled": s.EngineCounts.Sampled, "lost": s.EngineCounts.Lost, "rejected": s.EngineCounts.Rejected},
		"writtenEvents": s.WrittenEvents, "rejectedEvents": s.RejectedEvents, "writtenBytesBeforeSummary": s.WrittenBytesBeforeSummary}
	if s.Aggregates != nil {
		result["aggregates"] = aggregates(*s.Aggregates)
	}
	if s.Correlation != nil {
		result["correlation"] = fileCorrelation(*s.Correlation)
	}
	if s.OOMCorrelation != nil {
		result["oomCorrelation"] = oomCorrelation(*s.OOMCorrelation)
	}
	if s.KubernetesContext != nil {
		result["kubernetesContext"] = kubernetesContext(*s.KubernetesContext)
	}
	return result
}
func total(t traceaggregate.Total) any {
	return map[string]any{"value": t.Value, "unreported": t.Unreported, "overflow": t.Overflow}
}
func fileOperations(v traceaggregate.FileOperations) any {
	return map[string]any{"operations": v.Operations, "requestedBytes": total(v.RequestedBytes), "completedBytes": total(v.CompletedBytes)}
}
func cacheOperations(v traceaggregate.CacheOperations) any {
	return map[string]any{"operations": v.Operations, "pages": total(v.Pages)}
}
func aggregates(a traceaggregate.Summary) any {
	r := map[string]any{"kind": a.Kind, "observations": a.Observations}
	switch a.Kind {
	case trace.Files:
		r["reads"], r["writes"] = fileOperations(a.Reads), fileOperations(a.Writes)
	case trace.Cache:
		r["additions"], r["removals"] = cacheOperations(a.Additions), cacheOperations(a.Removals)
	case trace.OOM:
		r["decisions"] = map[string]uint64{"cgroup": a.OOM.Cgroup, "global": a.OOM.Global, "unknown": a.OOM.Unknown, "missingProcessContext": a.OOM.MissingProcessContext}
	}
	return r
}
func gauge(g trace.GaugePair) any    { return map[string]any{"before": g.Before, "after": g.After} }
func delta(d trace.CounterDelta) any { return map[string]any{"state": d.State, "delta": d.Delta} }
func window(w trace.CorrelationWindow) map[string]any {
	r := map[string]any{"state": w.State}
	if w.State == "overlapping" {
		r["evidenceStart"], r["beforeEnd"], r["afterStart"], r["evidenceEnd"] = w.EvidenceStart, w.BeforeEnd, w.AfterStart, w.EvidenceEnd
		r["overlapStart"], r["overlapEnd"], r["uncertaintyNanos"] = w.OverlapStart, w.OverlapEnd, w.Uncertainty
	}
	return r
}
func fileCorrelation(c trace.Correlation) any {
	r := window(trace.CorrelationWindow{State: c.State, EvidenceStart: c.EvidenceStart, BeforeEnd: c.BeforeEnd, AfterStart: c.AfterStart, EvidenceEnd: c.EvidenceEnd, OverlapStart: c.OverlapStart, OverlapEnd: c.OverlapEnd, Uncertainty: c.Uncertainty})
	if c.State == "overlapping" {
		r["fileBytes"], r["dirtyBytes"], r["writebackBytes"] = gauge(c.File), gauge(c.Dirty), gauge(c.Writeback)
		r["refault"], r["scan"], r["steal"] = delta(c.Refault), delta(c.Scan), delta(c.Steal)
	}
	return r
}
func events(d trace.OOMEventDeltas) any {
	return map[string]any{"low": delta(d.Low), "high": delta(d.High), "max": delta(d.Max), "oom": delta(d.OOM), "oomKill": delta(d.Kill), "oomGroupKill": delta(d.GroupKill)}
}
func limit(l trace.OOMLimit) any { return map[string]any{"state": l.State, "bytes": l.Bytes} }
func oomCorrelation(c trace.OOMCorrelation) any {
	r := map[string]any{"window": window(c.Window)}
	if c.Window.State == "overlapping" {
		r["local"], r["hierarchical"] = events(c.Local), events(c.Hierarchical)
		r["currentBytes"] = gauge(c.Current)
		r["limitBefore"], r["limitAfter"] = limit(c.LimitBefore), limit(c.LimitAfter)
		r["someStallMicros"], r["fullStallMicros"] = delta(c.PSISome), delta(c.PSIFull)
	}
	return r
}
func kubernetesContext(c trace.KubernetesOOMContext) any {
	r := map[string]any{"state": c.State}
	if c.State == "observed" {
		r["beforeStart"], r["beforeEnd"], r["afterStart"], r["afterEnd"] = c.BeforeStart, c.BeforeEnd, c.AfterStart, c.AfterEnd
		r["restarts"] = delta(c.Restarts)
		r["pressureBefore"], r["pressureAfter"] = c.PressureBefore, c.PressureAfter
	}
	return r
}
