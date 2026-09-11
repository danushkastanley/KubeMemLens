package nodeview

import (
	"fmt"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/capability"
	"github.com/danushkastanley/kube-memlens/internal/nodeanalysis"
	"github.com/danushkastanley/kube-memlens/internal/nodecontext"
)

func Lines(evidence api.NodeEvidence, now time.Time, width int) []string {
	a, record := evidence.Analysis, evidence.Record
	record.Freshness = RecordFreshness(record, now)
	lines := []string{"Node: " + a.NodeName, "Analysis: " + string(a.Severity) + "; confidence: " + string(a.Confidence),
		"Evaluated: " + sample(a.EvaluatedAt, now), "Source: " + string(a.Facts.Source) + "; availability: " + string(a.Facts.Availability),
		"Record freshness: " + string(record.Freshness) + "; received: " + sample(record.ReceivedAt, now)}
	if record.Report != nil {
		lines = append(lines, "Latest report: "+string(record.Report.Availability)+"; reason: "+string(record.Report.Reason), "Reported: "+sample(record.Report.ReportedAt, now))
	}
	if record.LastGood != nil {
		lines = append(lines, "Source completeness: "+string(record.LastGood.Evidence.Completeness), "Source sample: "+sample(record.LastGood.Evidence.CapturedAt, now))
		if record.LastGood.Stats != nil {
			lines = append(lines, "Node boot: "+instant(record.LastGood.Stats.StartedAt), "Provenance: "+string(record.LastGood.Stats.Provenance))
		}
		for _, caveat := range record.LastGood.Evidence.Caveats {
			lines = append(lines, "Source caveat: "+caveat)
		}
	} else {
		lines = append(lines, "Source completeness: unreported")
	}
	lines = append(lines, "", "Node memory (independent measurements; do not stack):")
	lines = append(lines, memoryLines("Node memory", a.Facts.Memory, now)...)
	lines = append(lines, swapLines("Node swap", a.Facts.Swap, now)...)
	lines = append(lines, "Swap allocation does not establish swap I/O.")
	if a.Facts.MajorFaultsPerSecond != nil {
		lines = append(lines, fmt.Sprintf("Major faults: %.2f/s; since %s", *a.Facts.MajorFaultsPerSecond, instant(a.Facts.FaultWindowStartedAt)), "Formula: "+a.Facts.MajorFaultFormula)
	}
	if a.Facts.GrowingSwapBytes != nil {
		lines = append(lines, "Swap allocation growth: "+bytes(a.Facts.GrowingSwapBytes)+"; since "+instant(a.Facts.SwapWindowStartedAt), "Formula: "+a.Facts.SwapGrowthFormula)
	}
	lines = append(lines, "", "Kubernetes Node context:")
	if c := a.Facts.Context; c != nil {
		lines = append(lines, "Status sampled: "+sample(c.CapturedAt, now), "Capacity: "+bytes(c.CapacityBytes), "Allocatable: "+bytes(c.AllocatableBytes), "MemoryPressure: "+c.MemoryPressure)
		lines = append(lines, "Hugepage pools (separate resources, not ordinary headroom):")
		if len(c.Hugepages) == 0 {
			lines = append(lines, "Hugepage pools: unreported")
		}
		for _, page := range c.Hugepages {
			lines = append(lines, page.Resource+": capacity "+bytes(page.CapacityBytes)+"; allocatable "+bytes(page.AllocatableBytes))
		}
	} else {
		lines = append(lines, "Capacity / allocatable / MemoryPressure / hugepages: unreported")
	}
	lines = append(lines, "", "System containers (may overlap):")
	if len(a.Facts.SystemContainers) == 0 {
		lines = append(lines, "System containers: unreported")
	}
	for _, system := range a.Facts.SystemContainers {
		lines = append(lines, "", string(system.Category)+" started: "+instant(system.StartedAt))
		lines = append(lines, memoryLines(string(system.Category), system.Memory, now)...)
		lines = append(lines, swapLines(string(system.Category)+" swap", system.Swap, now)...)
	}
	lines = append(lines, "", "Observed Pod charge: "+bytes(a.ObservedPodCharge), "Contributor access: "+string(a.ContributorAccess))
	if c := a.Coverage; c != nil {
		lines = append(lines, fmt.Sprintf("Cgroup coverage: %s; mapped %d; unmapped %d; Pods %d", c.State, c.MappedContainers, c.UnmappedContainers, c.Pods), "Cgroup sampled: "+sample(c.CapturedAt, now), "Formula: "+c.ObservedChargeFormula)
	}
	lines = append(lines, estimateLines("Outside observed Pods estimate", a.OutsidePods)...)
	lines = append(lines, estimateLines("Unaccounted estimate", a.Unaccounted)...)
	lines = append(lines, "", "Pressure evidence:")
	for _, signal := range a.Signals {
		value := ""
		if signal.Value != nil {
			value = fmt.Sprintf(" %.2f %s", *signal.Value, signal.Unit)
		}
		if signal.Count != nil {
			value = fmt.Sprintf(" %d %s", *signal.Count, signal.Unit)
		}
		lines = append(lines, string(signal.Severity)+": "+signal.Code+value+"; source "+string(signal.Source)+"; "+sample(signal.CapturedAt, now))
	}
	if len(a.Signals) == 0 {
		lines = append(lines, "Pressure evidence: unreported")
	}
	for _, caveat := range a.Caveats {
		lines = append(lines, "Caveat: "+string(caveat))
	}
	lines = append(lines, contributorLines(a.Rankings)...)
	return Wrap(lines, width)
}

func RecordFreshness(record api.NodeContextRecord, now time.Time) capability.Freshness {
	if record.LastGood != nil && now.Sub(record.LastGood.Evidence.CapturedAt) > nodecontext.StaleAfter {
		return capability.Stale
	}
	return record.Freshness
}

func estimateLines(label string, e nodeanalysis.Estimate) []string {
	lines := []string{label + ": " + bytes(e.Bytes) + " (" + string(e.State) + ")", "Source: " + e.Source, "Formula: " + e.Formula}
	if e.Bytes != nil {
		lines = append(lines, "Samples: "+instant(e.CapturedAt)+" / "+instant(e.ComparedAt), "Qualification evidence: "+e.Qualification)
	}
	for _, caveat := range e.Caveats {
		lines = append(lines, "Estimate caveat: "+string(caveat))
	}
	return lines
}

func contributorLines(r *nodeanalysis.Rankings) []string {
	if r == nil {
		return []string{"", "Contributor rankings: unavailable"}
	}
	lines := []string{"", "Contributors ranked by " + string(r.Metric) + "; source " + string(r.Source), "Formula: " + r.Formula}
	for _, group := range []struct {
		name string
		rows []nodeanalysis.Contributor
	}{{"Pods", r.Pods}, {"Workloads", r.Workloads}} {
		lines = append(lines, group.name+":")
		for index, row := range group.rows {
			psi := "unreported"
			if row.PSIFullAvg10 != nil {
				psi = fmt.Sprintf("%.2f%%", *row.PSIFullAvg10)
			}
			lines = append(lines, fmt.Sprintf("%d. %s/%s/%s", index+1, row.Namespace, row.Kind, row.Name),
				"  Total "+bytes(&row.Charge.Total)+"; anon "+bytes(&row.Charge.Anon)+"; cache "+bytes(&row.Charge.Cache),
				"  Shmem "+bytes(&row.Charge.Shmem)+"; residual "+bytes(&row.Charge.Residual)+"; PSI full avg10 "+psi+"; OOM kills "+count(row.OOMKills))
		}
		if len(group.rows) == 0 {
			lines = append(lines, "No mapped contributors.")
		}
	}
	if r.Truncated {
		lines = append(lines, fmt.Sprintf("Rankings limited to %d rows per group.", r.Limit))
	}
	return lines
}
