// Package traceview presents only validated numeric trace evidence.
package traceview

import (
	"fmt"
	"strings"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	"github.com/danushkastanley/kube-memlens/internal/traceaggregate"
	"github.com/danushkastanley/kube-memlens/internal/traceframe"
)

func SummaryLines(s traceframe.Summary) []string {
	lines := []string{
		fmt.Sprintf("termination: %s", s.Termination),
		fmt.Sprintf("incomplete evidence: %t", s.Incomplete),
		fmt.Sprintf("engine produced: %s; sampled: %s; lost: %s; rejected: %s", count(s.EngineCounts.Produced), count(s.EngineCounts.Sampled), count(s.EngineCounts.Lost), count(s.EngineCounts.Rejected)),
	}
	if s.Aggregates != nil {
		lines = append(lines, aggregateLines(*s.Aggregates)...)
	}
	if s.Correlation != nil {
		lines = append(lines, "memory correlation: "+s.Correlation.State)
	}
	if s.OOMCorrelation != nil {
		lines = append(lines, "OOM memory correlation: "+s.OOMCorrelation.Window.State)
	}
	if s.KubernetesContext != nil {
		lines = append(lines, "Kubernetes OOM context: "+s.KubernetesContext.State)
	}
	return lines
}
func aggregateLines(a traceaggregate.Summary) []string {
	switch a.Kind {
	case trace.Files:
		return []string{fmt.Sprintf("file reads: %d; writes: %d", a.Reads.Operations, a.Writes.Operations),
			"read bytes requested: " + total(a.Reads.RequestedBytes) + "; completed: " + total(a.Reads.CompletedBytes),
			"write bytes requested: " + total(a.Writes.RequestedBytes) + "; completed: " + total(a.Writes.CompletedBytes)}
	case trace.Cache:
		return []string{fmt.Sprintf("cache additions: %d; removals: %d", a.Additions.Operations, a.Removals.Operations),
			"pages added: " + total(a.Additions.Pages) + "; removed: " + total(a.Removals.Pages)}
	case trace.OOM:
		return []string{fmt.Sprintf("OOM decisions: cgroup %d; global %d; unknown %d", a.OOM.Cgroup, a.OOM.Global, a.OOM.Unknown), fmt.Sprintf("decisions missing process context: %d", a.OOM.MissingProcessContext)}
	default:
		return []string{"aggregate evidence: unreported"}
	}
}
func count(value *uint64) string {
	if value == nil {
		return "unreported"
	}
	return fmt.Sprint(*value)
}
func total(value traceaggregate.Total) string {
	var reasons []string
	if value.Unreported {
		reasons = append(reasons, "unreported")
	}
	if value.Overflow {
		reasons = append(reasons, "overflow")
	}
	if len(reasons) != 0 {
		return strings.Join(reasons, "; ")
	}
	return count(value.Value)
}
