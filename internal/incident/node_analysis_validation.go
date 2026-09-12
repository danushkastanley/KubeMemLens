package incident

import (
	"encoding/hex"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/capability"
	"github.com/danushkastanley/kube-memlens/internal/nodeanalysis"
	"github.com/danushkastanley/kube-memlens/internal/nodecontext"
)

func validateNodeAnalysis(a nodeanalysis.Analysis, redacted bool) error {
	if a.SchemaVersion != nodeanalysis.SchemaVersion || !oneOf(string(a.Severity), "unknown", "normal", "warning", "critical") || !oneOf(string(a.Confidence), "low", "medium", "high") || a.Facts.Source != nodecontext.Source || !availability(a.Facts.Availability) || len(a.Signals) > 16 || len(a.Caveats) > 32 {
		return fmt.Errorf("invalid Node analysis schema, state or bounds")
	}
	if a.Facts.Context != nil && a.Facts.ContextSource != capability.KubernetesStatus || a.Facts.Context == nil && a.Facts.ContextSource != "" {
		return fmt.Errorf("invalid Node context source")
	}
	if err := validateNodeCaveats(a.Caveats); err != nil {
		return err
	}
	if a.ContributorAccess != nodeanalysis.ClusterPods && a.ContributorAccess != nodeanalysis.NodeOnly {
		return fmt.Errorf("invalid Node contributor access")
	}
	if a.ContributorAccess == nodeanalysis.NodeOnly && (a.Coverage != nil || a.Rankings != nil || a.ObservedPodCharge != nil || a.OutsidePods.Bytes != nil || a.Unaccounted.Bytes != nil) {
		return fmt.Errorf("Node-only incident contains contributor evidence")
	}
	for _, s := range a.Signals {
		if s.CapturedAt.IsZero() || s.CapturedAt.After(a.EvaluatedAt.Add(30*time.Second)) || !oneOf(string(s.Severity), "normal", "warning", "critical") {
			return fmt.Errorf("invalid Node signal time or severity")
		}
		switch s.Code {
		case "kubernetes-memory-pressure", "kubernetes-no-memory-pressure":
			if s.Source != capability.KubernetesStatus || s.Value != nil || s.Count != nil {
				return fmt.Errorf("invalid Kubernetes pressure signal")
			}
		case "node-full-stall-10s", "node-sustained-full-stall-60s", "node-some-stall-10s", "node-low-reported-stall-10s":
			if s.Source != nodecontext.Source || s.Value == nil || !nodePercent(*s.Value) || s.Count != nil || s.Unit != "percent" {
				return fmt.Errorf("invalid Node PSI signal")
			}
		case "observed-pod-oom-kills":
			if a.ContributorAccess != nodeanalysis.ClusterPods || s.Source != capability.Cgroup || s.Value != nil || s.Unit != "events" {
				return fmt.Errorf("unauthorised or invalid OOM signal")
			}
		default:
			return fmt.Errorf("unknown Node pressure signal")
		}
	}
	if c := a.Coverage; c != nil {
		if c.Source != capability.Cgroup || !oneOf(string(c.State), "missing", "partial", "complete") || c.MappedContainers < 0 || c.UnmappedContainers < 0 || c.MappedContainers+c.UnmappedContainers > nodeanalysis.MaxContainers || c.Pods < 0 || c.Pods > c.MappedContainers || c.ObservedChargeFormula != "sum(mapped container memory.current)" {
			return fmt.Errorf("invalid contributor coverage")
		}
		if c.State == nodeanalysis.Complete && (c.CapturedAt.IsZero() || c.UnmappedContainers > 0) {
			return fmt.Errorf("incomplete contributors claim complete coverage")
		}
	}
	if a.ObservedPodCharge != nil && (a.Coverage == nil || a.Coverage.State == nodeanalysis.Missing) {
		return fmt.Errorf("observed charge requires cgroup coverage")
	}
	if r := a.Rankings; r != nil {
		if a.Coverage == nil || a.ObservedPodCharge == nil || r.Source != capability.Cgroup || !oneOf(string(r.Metric), "total", "anon", "cache", "shmem", "residual", "psi", "oom") || r.Limit < 1 || r.Limit > nodeanalysis.MaxContributors || len(r.Pods) > r.Limit || len(r.Workloads) > r.Limit || len(r.Formula) > 256 {
			return fmt.Errorf("invalid contributor rankings")
		}
		if err := validateNodeRows(r.Pods, redacted, "pod"); err != nil {
			return err
		}
		if err := validateNodeRows(r.Workloads, redacted, "workload"); err != nil {
			return err
		}
	}
	for _, item := range []struct {
		e       nodeanalysis.Estimate
		formula string
	}{{a.OutsidePods, "max(0, node.usage - observedPodCharge)"}, {a.Unaccounted, "max(0, node.usage - observedPodCharge - qualifiedDisjointSystemUsage)"}} {
		e := item.e
		if e.Formula != item.formula || e.Source != "kubelet-summary + cgroup" || !oneOf(string(e.State), "available", "unreported") || (e.Bytes != nil) != (e.State == capability.Available) {
			return fmt.Errorf("invalid Node estimate contract")
		}
		if err := validateNodeCaveats(e.Caveats); err != nil {
			return err
		}
		if e.Bytes != nil {
			digest, err := hex.DecodeString(e.Qualification)
			if err != nil || len(digest) != 32 || e.Qualification != strings.ToLower(e.Qualification) || e.CapturedAt.IsZero() || e.ComparedAt.IsZero() || a.Coverage == nil || a.Coverage.State != nodeanalysis.Complete || a.ObservedPodCharge == nil {
				return fmt.Errorf("Node estimate lacks qualification or complete evidence")
			}
		}
	}
	if v := a.Facts.MajorFaultsPerSecond; v != nil && (!finiteNodeNumber(*v) || *v < 0 || a.Facts.Memory == nil || !nodeRateWindow(a.Facts.FaultWindowStartedAt, a.Facts.Memory.CapturedAt) || a.Facts.MajorFaultFormula != "(current majorPageFaults - previous majorPageFaults) / elapsedSeconds") {
		return fmt.Errorf("invalid Node fault rate")
	}
	if a.Facts.GrowingSwapBytes != nil && (a.Facts.Swap == nil || !nodeRateWindow(a.Facts.SwapWindowStartedAt, a.Facts.Swap.CapturedAt) || a.Facts.SwapGrowthFormula != "max(0, current swap.usage - previous swap.usage)") {
		return fmt.Errorf("invalid Node swap growth")
	}
	return nil
}

func validateNodeRows(rows []nodeanalysis.Contributor, redacted bool, prefix string) error {
	seen := map[string]bool{}
	for _, r := range rows {
		key := r.Namespace + "/" + r.Kind + "/" + r.Name + "/" + r.UID
		if !dns(r.Namespace) || len(r.Namespace) > 63 || !dns(r.Name) || r.Kind == "" || len(r.Kind) > 63 || len(r.UID) > 128 || seen[key] {
			return fmt.Errorf("invalid contributor identity")
		}
		seen[key] = true
		if redacted && (r.UID != "" || !nodeAliasPattern.MatchString(r.Namespace) || !strings.HasPrefix(r.Namespace, "namespace-") || !nodeAliasPattern.MatchString(r.Name) || !strings.HasPrefix(r.Name, prefix+"-") || prefix == "workload" && r.Kind != "Workload") {
			return fmt.Errorf("redacted Node incident contains contributor identity")
		}
		if prefix == "pod" && r.Kind != "Pod" {
			return fmt.Errorf("invalid Pod contributor kind")
		}
		if r.PSIFullAvg10 != nil && !nodePercent(*r.PSIFullAvg10) {
			return fmt.Errorf("invalid contributor PSI")
		}
	}
	return nil
}

func validateNodeCaveats(values []nodeanalysis.Caveat) error {
	if len(values) > 32 {
		return fmt.Errorf("Node caveat limit exceeded")
	}
	for _, v := range values {
		switch v {
		case nodeanalysis.SourceMissing, nodeanalysis.SourceFailed, nodeanalysis.SourceStale, nodeanalysis.AgentMissing, nodeanalysis.AgentPartial, nodeanalysis.AgentStale, nodeanalysis.Unmapped, nodeanalysis.IdentityMismatch, nodeanalysis.Skewed, nodeanalysis.NegativeGap, nodeanalysis.Overflow, nodeanalysis.Unqualified, nodeanalysis.SystemOverlap, nodeanalysis.PodAccessDenied, nodeanalysis.HugepagesSeparate, nodeanalysis.CounterReset, nodeanalysis.SwapNotIO, nodeanalysis.NoPressureEvidence, nodeanalysis.CompositionOverlap:
		default:
			return fmt.Errorf("unknown Node analysis caveat")
		}
	}
	return nil
}

func finiteNodeNumber(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
func nodePercent(v float64) bool      { return finiteNodeNumber(v) && v >= 0 && v <= 100 }
func nodeRateWindow(start, end time.Time) bool {
	return !start.IsZero() && end.After(start) && end.Sub(start) <= 2*time.Minute
}
