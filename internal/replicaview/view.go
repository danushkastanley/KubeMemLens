package replicaview

import (
	"fmt"
	"strings"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/replicabaseline"
)

// Lines is shared by CLI and TUI so confidence and abstentions use the same copy.
func Lines(r replicabaseline.Report, now time.Time) []string {
	if err := r.Validate(); err != nil {
		return []string{"Replica evidence is invalid; refresh the selection."}
	}
	lines := []string{fmt.Sprintf("Replica comparison · %s/%s · %s", r.Workload.Namespace, r.Workload.Name, r.Workload.Kind), "Informational only. Four other comparable replicas are required.", "Reference method: median, range and median absolute deviation (MAD).", "Flag requires >25% difference and the metric effect floor; non-flat peers also require |modified score| >3.5."}
	if now.Sub(r.ObservedAt) > replicabaseline.FreshFor {
		return append(lines, "This comparison is stale. Refresh before interpreting replica differences.")
	}
	if len(r.Peers) == 0 {
		return append(lines, "No authorised current replica members were reported.")
	}
	for _, p := range r.Peers {
		lines = append(lines, "", p.Peer.Name)
		if p.Exclusion != "" {
			lines = append(lines, "  Excluded: "+reason(p.Exclusion))
			continue
		}
		lines = append(lines, fmt.Sprintf("  Revision %s · sampled %s", p.Revision, p.CapturedAt.UTC().Format(time.RFC3339)))
		for _, c := range p.Comparisons {
			label := metricLabel(c.Metric)
			if c.State != "compared" {
				lines = append(lines, fmt.Sprintf("  %s: %s (%d references)", label, reason(c.State), len(c.References)))
				continue
			}
			outcome := "within comparison thresholds"
			if c.Outlier != "none" {
				outcome = c.Outlier + " than peers"
			}
			lines = append(lines, fmt.Sprintf("  %s: %s · %s", label, number(c.Metric, *c.Candidate), outcome))
			lines = append(lines, fmt.Sprintf("    Peer median %s; range %s–%s; difference %s; MAD %s", number(c.Metric, c.Distribution.Median), number(c.Metric, c.Distribution.Minimum), number(c.Metric, c.Distribution.Maximum), number(c.Metric, *c.Difference), number(c.Metric, c.Distribution.MAD)))
			lines = append(lines, "    References: "+peerNames(r, c.References)+" · "+reason(c.Confidence))
			if c.ModifiedScore != nil {
				lines = append(lines, fmt.Sprintf("    Modified score %.2f", *c.ModifiedScore))
			} else {
				lines = append(lines, "    Flat reference values: practical difference threshold used; no statistical score.")
			}
			for _, o := range c.Omitted {
				lines = append(lines, "    Omitted "+peerNames(r, []string{o.PeerUID})+": "+reason(o.Reason))
			}
		}
		for _, excluded := range p.Excluded {
			lines = append(lines, "  Excluded reference "+excluded.Peer.Name+": "+reason(excluded.Reason))
		}
	}
	lines = append(lines, "", "Next checks: compare traffic and work performed, then inspect memory composition, pressure and recent changes.", "Effect floors: charge/swap 16 MiB; composition/limit 10 percentage points; growth 1 MiB/min; PSI some/full 1/0.1 percentage points; events 1/min.", "Limit usage and PSI are maximum-container values; they do not measure Pod ancestor enforcement.", "Zero composition fields without presence metadata remain unreported. Event rates require known local deltas.")
	return append(lines, r.Caveats...)
}

func peerNames(r replicabaseline.Report, uids []string) string {
	names := make([]string, 0, len(uids))
	for _, uid := range uids {
		for _, p := range r.Peers {
			if p.Peer.UID == uid {
				names = append(names, p.Peer.Name)
			}
		}
	}
	return strings.Join(names, ", ")
}
func number(metric replicabaseline.Metric, n float64) string {
	switch metric {
	case replicabaseline.Charge, replicabaseline.Swap:
		return fmt.Sprintf("%.2f MiB", n/(1<<20))
	case replicabaseline.ChargeSlope:
		return fmt.Sprintf("%.2f MiB/min", n*60/(1<<20))
	case replicabaseline.OOMRate, replicabaseline.OOMKillRate, replicabaseline.HighRate, replicabaseline.MaxRate:
		return fmt.Sprintf("%.2f/min", n*60)
	case replicabaseline.PSISome, replicabaseline.PSIFull:
		return fmt.Sprintf("%.2f%%", n)
	default:
		return fmt.Sprintf("%.2f%%", n*100)
	}
}
func metricLabel(metric replicabaseline.Metric) string {
	labels := map[replicabaseline.Metric]string{replicabaseline.Charge: "Cgroup charge", replicabaseline.AnonFraction: "Anonymous fraction", replicabaseline.FileFraction: "File-cache fraction", replicabaseline.ShmemFraction: "Shared-memory fraction", replicabaseline.KernelFraction: "Kernel fraction", replicabaseline.Swap: "Swap", replicabaseline.ChargeSlope: "Charge growth", replicabaseline.LimitUsage: "Maximum container limit usage", replicabaseline.PSISome: "Maximum container PSI some", replicabaseline.PSIFull: "Maximum container PSI full", replicabaseline.OOMRate: "OOM event rate", replicabaseline.OOMKillRate: "OOM-kill event rate", replicabaseline.HighRate: "Memory-high event rate", replicabaseline.MaxRate: "Memory-max event rate"}
	return labels[metric]
}
func reason(value string) string {
	labels := map[string]string{"insufficient-peers": "insufficient comparable peers", "unreported": "unreported", "partial": "partial evidence", "stale": "stale evidence", "unavailable": "unavailable", "limited-change-history": "limited confidence: change history is incomplete", "current-peers": "current comparable evidence; not a probability", "recent-or-unreported-stability": "recent change or less than five minutes of verified continuity", "adverse-reference-evidence": "pressure, limit usage or recent adverse events", "revision-mismatch": "different revision", "container-shape-mismatch": "different container or resource layout", "source-clock-skew": "samples taken too far apart", "delta-window-mismatch": "different counter intervals"}
	if label, found := labels[value]; found {
		return label
	}
	return strings.ReplaceAll(value, "-", " ")
}
