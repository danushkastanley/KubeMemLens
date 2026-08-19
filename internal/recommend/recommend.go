package recommend

import "github.com/danushkastanley/kube-memlens/internal/explain"

type Recommendation struct {
	ID         string   `json:"id"`
	Priority   string   `json:"priority"`
	Action     string   `json:"action"`
	Rationale  string   `json:"rationale"`
	Conditions []string `json:"conditions"`
}

func ForFinding(finding explain.Result) []Recommendation {
	primary := Recommendation{Priority: "investigate"}
	switch finding.Diagnosis {
	case explain.DiagnosisOOMRisk:
		primary.ID = "investigate-oom-evidence"
		primary.Action = "Preserve the incident window, inspect the dominant bucket, and compare peak with the configured limit."
		primary.Rationale = "Recent OOM or hard-limit evidence is actionable, but current usage can be low after a restart and does not identify the cause by itself."
		primary.Conditions = []string{"Use restart reason, peak, event deltas, and workload history together.", "Consider more headroom only after identifying whether the spike was anon, cache, shmem, or kernel memory."}
	case explain.DiagnosisPressure:
		primary.ID = "investigate-reclaim-impact"
		primary.Action = "Correlate PSI and memory.high events with latency, throughput, node pressure, and replica outliers."
		primary.Rationale = "Pressure signals show direct reclaim impact; composition determines whether sizing, cache behaviour, or node contention is the safer next investigation."
		primary.Conditions = []string{"Confirm the signal persists across multiple points.", "Do not change a Pod limit solely because the Node reports MemoryPressure."}
	case explain.DiagnosisLimitRisk:
		primary.ID = "validate-limit-headroom"
		primary.Action = "Compare current usage, peak, bounded growth, and the dominant bucket before considering additional limit headroom."
		primary.Rationale = "Low headroom raises transient-failure risk, but it does not prove the existing limit is wrong."
		primary.Conditions = []string{"Check restart and OOM history.", "Prefer workload-specific profiling or cache/tmpfs investigation before a limit change."}
	case explain.DiagnosisRSSHeavy:
		primary.ID = "profile-anonymous-memory"
		primary.Action = "Capture runtime heap or allocator evidence and compare it with RSS/anon growth."
		primary.Rationale = "Anonymous memory dominates, which is compatible with heap or native allocation growth but is not proof of a leak."
		primary.Conditions = []string{"Look for sustained growth across bounded history.", "Consider headroom only when growth or pressure is repeatable and workload demand justifies it."}
	case explain.DiagnosisCacheHeavy:
		primary.ID = "inspect-file-cache"
		primary.Action = "Inspect file access and application cache policy; size application heap from anon evidence rather than total charge."
		primary.Rationale = "Filesystem-backed cache is a major charge and may be reclaimable when there is no PSI or refault pressure."
		primary.Conditions = []string{"Check refault and PSI before treating cache as harmless.", "Do not reduce limits aggressively when active working-set cache is serving the workload."}
	case explain.DiagnosisTmpfsHeavy:
		primary.ID = "bound-memory-backed-storage"
		primary.Action = "Inspect memory-backed emptyDir volumes and /dev/shm, then apply an intentional size limit where the workload contract permits it."
		primary.Rationale = "Tmpfs and shared memory consume cgroup memory even though they look like files."
		primary.Conditions = []string{"Confirm which process or volume owns the data.", "Protect required IPC and model/data working sets from arbitrary truncation."}
	case explain.DiagnosisSlabHeavy:
		primary.ID = "inspect-kernel-facing-behaviour"
		primary.Action = "Inspect socket churn, connection counts, file traversal, and inode/dentry pressure."
		primary.Rationale = "Kernel memory is meaningful; changing application heap settings may not address it."
		primary.Conditions = []string{"Compare with node-level slab and network metrics.", "Escalate persistent growth with kernel/runtime evidence."}
	case explain.DiagnosisDirtyWritebackHeavy:
		primary.ID = "inspect-writeback-path"
		primary.Action = "Correlate dirty/writeback memory with write throughput, filesystem latency, and node storage pressure."
		primary.Rationale = "Buffered writes and slow flushes can raise charged memory without application heap retention."
		primary.Conditions = []string{"Inspect the storage path before changing memory resources.", "Check whether the pattern is bursty or sustained."}
	default:
		primary.ID = "observe-bounded-history"
		primary.Priority = "observe"
		primary.Action = "Keep the current resource configuration and compare bounded history with workload symptoms."
		primary.Rationale = "The current snapshot has no single dominant risk signal."
		primary.Conditions = []string{"Absence of a current signal does not rule out a transient incident.", "Capture evidence before making resource changes."}
	}
	return []Recommendation{primary, {
		ID: "no-automatic-mutation", Priority: "safety",
		Action:     "Keep remediation read-only; review and apply any resource or volume change through the workload's normal delivery process.",
		Rationale:  "KubeMemLens explains evidence but does not know application SLOs, traffic forecasts, or deployment policy.",
		Conditions: []string{"Validate on every replica and retain a rollback path."},
	}}
}
