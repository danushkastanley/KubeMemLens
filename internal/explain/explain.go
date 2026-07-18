package explain

import (
	"fmt"
	"strings"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/model"
)

type Diagnosis string
type Confidence string

const (
	DiagnosisCacheHeavy          Diagnosis = "cache-heavy"
	DiagnosisRSSHeavy            Diagnosis = "rss-heavy"
	DiagnosisTmpfsHeavy          Diagnosis = "tmpfs-heavy"
	DiagnosisDirtyWritebackHeavy Diagnosis = "dirty-writeback-heavy"
	DiagnosisSlabHeavy           Diagnosis = "slab-heavy"
	DiagnosisOOMRisk             Diagnosis = "oom-risk"
	DiagnosisPressure            Diagnosis = "memory-pressure"
	DiagnosisLimitRisk           Diagnosis = "limit-risk"
	DiagnosisMixed               Diagnosis = "mixed"
	DiagnosisNormal              Diagnosis = "normal"
)

const (
	ConfidenceHigh   Confidence = "high"
	ConfidenceMedium Confidence = "medium"
	ConfidenceLow    Confidence = "low"
)

type Result struct {
	Diagnosis         Diagnosis
	Severity          Severity
	Summary           string
	LikelyExplanation string
	SuggestedChecks   []string
	Signals           []string
	Confidence        Confidence
	ConfidenceReason  string
	Caveats           []string
	EvidenceWindow    EvidenceWindow
}

func AnalyzePod(pod api.PodSnapshot) Result {
	return AnalyzePodAt(pod, time.Now().UTC())
}

func AnalyzePodAt(pod api.PodSnapshot, now time.Time) Result {
	if recentOOMTermination(pod.Context, now) {
		window := now.Sub(pod.Context.LastTerminationFinishedAt).Round(time.Second)
		return attachPodEvidenceWindow(finaliseResult(Result{
			Diagnosis: DiagnosisOOMRisk,
			Summary:   "A container in this Pod was recently terminated by the OOM killer.",
			LikelyExplanation: "Kubernetes termination state provides recent OOM evidence even when the replacement container has a new cgroup and reset counters. " +
				"Use current composition, peak, and limit headroom to investigate the new instance without assuming an application leak.",
			SuggestedChecks: []string{
				"Inspect the terminated container logs and events around the OOM timestamp.",
				"Compare configured limits with memory.peak and the dominant memory bucket.",
				"Check whether the restart followed a deploy, traffic burst, or node pressure event.",
			},
			Signals: []string{
				fmt.Sprintf("last termination reason=%s exit=%d", pod.Context.LastTerminationReason, pod.Context.LastTerminationExitCode),
				fmt.Sprintf("termination occurred %s ago", window),
			},
			Confidence:       ConfidenceHigh,
			ConfidenceReason: "Kubernetes recorded a recent OOMKilled termination for this Pod.",
		}, pod.Memory), pod)
	}
	result := Analyze(pod.Memory)
	if strings.EqualFold(pod.Context.NodeMemoryPressure, "True") {
		result.Signals = append(result.Signals, "Kubernetes Node condition MemoryPressure=True")
		if pod.Context.NodeMemoryAllocatableKnown {
			result.Signals = append(result.Signals, fmt.Sprintf("node allocatable memory=%s", model.FormatCompactBytes(pod.Context.NodeMemoryAllocatable)))
		}
		if result.Diagnosis == DiagnosisNormal {
			result.Diagnosis = DiagnosisPressure
			result.Summary = "The node is reporting memory pressure."
			result.LikelyExplanation = "The container cgroup does not show a stronger local diagnosis, but the Kubernetes Node condition indicates node-wide memory pressure that can cause reclaim or eviction impact."
			result.SuggestedChecks = []string{
				"Inspect node allocatable memory, eviction events, and the largest neighbouring workloads.",
				"Compare node MemoryPressure transition time with workload symptoms.",
				"Do not change this Pod's limit solely from the node condition; inspect local composition and pressure first.",
			}
			result.Confidence = ConfidenceMedium
			result.ConfidenceReason = "The Node condition is direct evidence, but it does not identify this Pod as the cause."
		}
	}
	if pod.Context.MemoryLimitContainers == len(pod.Containers) && pod.Context.MemoryLimitBytes > 0 {
		result.Signals = append(result.Signals, fmt.Sprintf("configured container limits total %s", model.FormatCompactBytes(pod.Context.MemoryLimitBytes)))
	}
	if pod.Context.RestartCount > 0 {
		result.Signals = append(result.Signals, fmt.Sprintf("Pod containers report %d restarts", pod.Context.RestartCount))
	}
	if pod.Context.OwnerKind != "" && pod.Context.OwnerName != "" {
		result.Signals = append(result.Signals, fmt.Sprintf("direct owner %s/%s", pod.Context.OwnerKind, pod.Context.OwnerName))
	}
	if pod.Context.WorkloadKind != "" && pod.Context.WorkloadName != "" {
		result.Signals = append(result.Signals, fmt.Sprintf("workload %s/%s", pod.Context.WorkloadKind, pod.Context.WorkloadName))
	}
	if pod.Context.RuntimeClassName != "" {
		result.Signals = append(result.Signals, fmt.Sprintf("runtimeClass=%s", pod.Context.RuntimeClassName))
	}
	if pod.Context.MemoryEmptyDirCount > 0 {
		unbounded := pod.Context.MemoryEmptyDirCount - pod.Context.MemoryEmptyDirLimited
		result.Signals = append(result.Signals, fmt.Sprintf(
			"memory-backed emptyDir volumes=%d; limited=%d; unbounded=%d; known limits=%s",
			pod.Context.MemoryEmptyDirCount, pod.Context.MemoryEmptyDirLimited, unbounded,
			model.FormatCompactBytes(pod.Context.MemoryEmptyDirLimitBytes),
		))
		if result.Diagnosis == DiagnosisTmpfsHeavy && unbounded > 0 {
			result.SuggestedChecks = append([]string{"Review the unbounded memory-backed emptyDir volume before increasing memory headroom."}, result.SuggestedChecks...)
		}
	}
	if pod.Memory.ReclaimDeltasKnown {
		efficiency := "n/a"
		if pod.Memory.PageScanDelta > 0 {
			efficiency = fmt.Sprintf("%.0f%%", float64(pod.Memory.PageStealDelta)/float64(pod.Memory.PageScanDelta)*100)
		}
		result.Signals = append(result.Signals, fmt.Sprintf(
			"recent reclaim scan=%d steal=%d efficiency=%s; refault=%d; major faults=%d",
			pod.Memory.PageScanDelta, pod.Memory.PageStealDelta, efficiency,
			pod.Memory.RefaultAnonDelta+pod.Memory.RefaultFileDelta, pod.Memory.MajorPageFaultsDelta,
		))
	}
	return attachPodEvidenceWindow(finaliseResult(result, pod.Memory), pod)
}

func recentOOMTermination(context api.PodContext, now time.Time) bool {
	if !context.LastTerminationKnown || !strings.EqualFold(context.LastTerminationReason, "OOMKilled") || context.LastTerminationFinishedAt.IsZero() {
		return false
	}
	age := now.Sub(context.LastTerminationFinishedAt)
	return age >= 0 && age <= 15*time.Minute
}

func Analyze(m model.MemoryBreakdown) Result {
	if m.HasOOMRisk() {
		return withConfidence(oomRisk(m), m)
	}
	if m.HasPressureRisk() {
		return withConfidence(pressureRisk(m), m)
	}
	if m.HasLimitRisk() {
		return withConfidence(limitRisk(m), m)
	}

	dirtyHigh := m.TotalBytes > 0 && (m.DirtyWritebackRatio() >= 0.10 || m.DirtyWritebackBytes() >= 256*mib)
	tmpfsHigh := m.TotalBytes > 0 && m.ShmemRatio() >= 0.30
	slabHigh := m.TotalBytes > 0 && (m.SlabRatio() >= 0.20 || componentRatio(m.SlabUnreclaimableBytes, m.TotalBytes) >= 0.10 || componentRatio(m.SocketBytes, m.TotalBytes) >= 0.10 || componentRatio(m.PageTableBytes, m.TotalBytes) >= 0.10)
	rssHigh := m.TotalBytes > 0 && m.AnonRatio() >= 0.65
	cacheHigh := m.TotalBytes > 0 && m.FileCacheRatio() >= 0.40 && m.AnonRatio() <= 0.50

	majorSignals := countTrue(dirtyHigh, tmpfsHigh, slabHigh, rssHigh, cacheHigh)
	if majorSignals > 1 && !dirtyHigh && !tmpfsHigh && !rssHigh {
		return withConfidence(mixed(m), m)
	}

	switch {
	case dirtyHigh:
		return withConfidence(dirtyWritebackHeavy(m), m)
	case tmpfsHigh:
		return withConfidence(tmpfsHeavy(m), m)
	case slabHigh:
		return withConfidence(slabHeavy(m), m)
	case rssHigh:
		return withConfidence(rssHeavy(m), m)
	case cacheHigh:
		return withConfidence(cacheHeavy(m), m)
	default:
		return withConfidence(normal(m), m)
	}
}

func withConfidence(result Result, memory model.MemoryBreakdown) Result {
	switch result.Diagnosis {
	case DiagnosisOOMRisk:
		if memory.EventDeltasKnown || memory.LocalEventDeltasKnown {
			result.Confidence = ConfidenceHigh
			result.ConfidenceReason = "A recent cgroup memory-event delta directly supports this diagnosis."
		} else {
			result.Confidence = ConfidenceMedium
			result.ConfidenceReason = "A cumulative cgroup event supports the diagnosis, but its occurrence may predate this sample."
		}
	case DiagnosisPressure:
		result.Confidence = ConfidenceHigh
		result.ConfidenceReason = "Recent PSI or memory.high evidence directly shows reclaim impact."
	case DiagnosisLimitRisk:
		result.Confidence = ConfidenceMedium
		result.ConfidenceReason = "Current usage and memory.max show low headroom, but a future limit breach is not certain."
	case DiagnosisNormal:
		result.Confidence = ConfidenceLow
		result.ConfidenceReason = "One snapshot found no dominant signal; absence of evidence is not evidence of healthy history."
	default:
		result.Confidence = ConfidenceMedium
		result.ConfidenceReason = "The composition is measured directly, while the likely cause remains a single-snapshot heuristic."
	}
	return finaliseResult(result, memory)
}

func oomRisk(m model.MemoryBreakdown) Result {
	signals := []string{}
	oom, oomKill, high, maxEvents := m.RecentEventCounts()
	source := "memory.events"
	if m.EventDeltasKnown {
		source += " delta"
	}
	if oom > 0 {
		signals = append(signals, fmt.Sprintf("%s oom=%d", source, oom))
	}
	if oomKill > 0 {
		signals = append(signals, fmt.Sprintf("%s oom_kill=%d", source, oomKill))
	}
	if maxEvents > 0 {
		signals = append(signals, fmt.Sprintf("%s max=%d", source, maxEvents))
	}
	if high > 0 {
		signals = append(signals, fmt.Sprintf("%s high=%d", source, high))
	}
	if m.PeakKnown {
		signals = append(signals, fmt.Sprintf("memory.peak=%s; current=%s", model.FormatCompactBytes(m.PeakBytes), model.FormatCompactBytes(m.TotalBytes)))
	}
	if m.MaxKnown && !m.MaxUnlimited {
		signals = append(signals, fmt.Sprintf("current usage is %.0f%% of memory.max", m.LimitUsageRatio()*100))
	}

	return Result{
		Diagnosis: DiagnosisOOMRisk,
		Summary:   "Memory pressure or OOM events were reported for this cgroup.",
		LikelyExplanation: "Based on available cgroup stats, this workload has crossed memory pressure or limit signals. " +
			"Check whether recent restarts, throttling, or limit pressure line up with the elevated memory number.",
		SuggestedChecks: []string{
			"Check pod events and container restart reasons.",
			"Compare memory.current with the configured container limit.",
			"Review recent deploys, traffic shifts, and workload-specific allocation changes.",
		},
		Signals: signals,
	}
}

func pressureRisk(m model.MemoryBreakdown) Result {
	_, _, high, _ := m.RecentEventCounts()
	signals := []string{}
	if high > 0 {
		source := "memory.events high"
		if m.LocalEventsKnown {
			source = "memory.events.local high"
		}
		signals = append(signals, fmt.Sprintf("%s delta=%d", source, high))
	}
	if m.PressureKnown {
		signals = append(signals,
			fmt.Sprintf("memory PSI some avg10=%.2f%%", m.PSISomeAvg10),
			fmt.Sprintf("memory PSI full avg10=%.2f%%", m.PSIFullAvg10),
		)
	}
	if m.SwapCurrentKnown && m.SwapCurrentBytes > 0 {
		signals = append(signals, fmt.Sprintf("swap current=%s", model.FormatCompactBytes(m.SwapCurrentBytes)))
	}
	return Result{
		Diagnosis:         DiagnosisPressure,
		Summary:           "The cgroup is showing recent reclaim throttling or memory stalls.",
		LikelyExplanation: "This is direct evidence that memory availability is affecting the workload. Composition still matters, but pressure and throttling should be investigated before sizing from total usage alone.",
		SuggestedChecks: []string{
			"Compare memory.current, memory.high, and memory.max for this container.",
			"Check whether PSI and high-event deltas align with latency or throughput degradation.",
			"Inspect node MemoryPressure and neighbouring workload activity during the same window.",
		},
		Signals: signals,
	}
}

func limitRisk(m model.MemoryBreakdown) Result {
	headroom := m.MaxBytes - minUint64(m.TotalBytes, m.MaxBytes)
	signals := []string{
		fmt.Sprintf("current usage is %.0f%% of memory.max", m.LimitUsageRatio()*100),
		fmt.Sprintf("remaining cgroup headroom is %s", model.FormatCompactBytes(headroom)),
	}
	if m.PeakKnown {
		signals = append(signals, fmt.Sprintf("memory.peak=%s", model.FormatCompactBytes(m.PeakBytes)))
	}
	return Result{
		Diagnosis:         DiagnosisLimitRisk,
		Summary:           "Current memory is close to the cgroup hard limit.",
		LikelyExplanation: "No recent OOM or pressure event is visible, but the remaining headroom is small. A transient allocation or cache burst could cross the limit before the next sample.",
		SuggestedChecks: []string{
			"Compare memory.peak with the current limit and recent traffic peaks.",
			"Check container restarts and the last termination reason.",
			"Investigate the dominant memory bucket before changing the limit.",
		},
		Signals: signals,
	}
}

func cacheHeavy(m model.MemoryBreakdown) Result {
	signals := []string{
		fmt.Sprintf("filesystem-backed cache, excluding shmem, is %.0f%% of total charged memory", m.FileCacheRatio()*100),
		fmt.Sprintf("RSS/anon is %.0f%% of total charged memory", m.AnonRatio()*100),
	}
	if m.FileBytes > 0 && float64(m.ActiveFileBytes)/float64(m.FileBytes) >= 0.60 {
		signals = append(signals, "active_file is high relative to the raw cgroup file value")
	}

	return Result{
		Diagnosis: DiagnosisCacheHeavy,
		Summary:   "File-backed cache is the largest visible contributor.",
		LikelyExplanation: "This does not look like a pure application heap leak. RSS/anonymous memory is relatively stable/low " +
			"compared to total memory, while file cache is high and active.",
		SuggestedChecks: []string{
			"Look for app-level disk caches with long TTLs.",
			"Check whether files under cache directories are being repeatedly read/written.",
			"Check emptyDir usage and sizeLimit.",
			"Consider scaling on RSS/anon instead of total working set for this workload.",
		},
		Signals: signals,
	}
}

func rssHeavy(m model.MemoryBreakdown) Result {
	return Result{
		Diagnosis: DiagnosisRSSHeavy,
		Summary:   "RSS/anonymous memory dominates the cgroup charge.",
		LikelyExplanation: "This suggests application heap, native anonymous memory, runtime heap growth, or retained allocations. " +
			"Use runtime-specific heap tooling before assuming page cache is the cause.",
		SuggestedChecks: []string{
			"Compare runtime heap metrics with RSS/anon.",
			"Capture a heap profile or allocator profile during the high-memory window.",
			"Check for recent code paths that retain large buffers or caches in process memory.",
		},
		Signals: []string{
			fmt.Sprintf("RSS/anon is %.0f%% of total charged memory", m.AnonRatio()*100),
		},
	}
}

func tmpfsHeavy(m model.MemoryBreakdown) Result {
	return Result{
		Diagnosis: DiagnosisTmpfsHeavy,
		Summary:   "Shared memory or tmpfs-backed memory is high.",
		LikelyExplanation: "This suggests memory-backed files such as tmpfs, /dev/shm, memory-backed emptyDir volumes, " +
			"or shared memory segments are a major part of the cgroup charge.",
		SuggestedChecks: []string{
			"Check memory-backed emptyDir volumes and sizeLimit values.",
			"Inspect /dev/shm and tmpfs usage inside the container.",
			"Look for workloads that spill queues, model files, or intermediate data to memory-backed filesystems.",
		},
		Signals: []string{
			fmt.Sprintf("shmem/tmpfs is %.0f%% of total charged memory", m.ShmemRatio()*100),
		},
	}
}

func dirtyWritebackHeavy(m model.MemoryBreakdown) Result {
	return Result{
		Diagnosis: DiagnosisDirtyWritebackHeavy,
		Summary:   "Dirty or writeback pages are high.",
		LikelyExplanation: "This suggests a write-heavy workload, dirty pages waiting for flush, slow writeback, or storage pressure. " +
			"File cache may still be involved, but the dirty/writeback signal makes the write path worth checking first.",
		SuggestedChecks: []string{
			"Check write throughput and storage latency during the incident window.",
			"Look for large buffered writes or temporary files.",
			"Review node-level disk pressure and filesystem metrics.",
		},
		Signals: []string{
			fmt.Sprintf("dirty/writeback is %.0f%% of total charged memory", m.DirtyWritebackRatio()*100),
		},
	}
}

func slabHeavy(m model.MemoryBreakdown) Result {
	signals := []string{fmt.Sprintf("slab memory is %.0f%% of total charged memory", m.SlabRatio()*100)}
	for _, detail := range []struct {
		name  string
		bytes uint64
	}{
		{name: "unreclaimable slab", bytes: m.SlabUnreclaimableBytes},
		{name: "socket memory", bytes: m.SocketBytes},
		{name: "page tables", bytes: m.PageTableBytes},
	} {
		if detail.bytes > 0 {
			signals = append(signals, fmt.Sprintf("%s is %.0f%% of total charged memory", detail.name, componentRatio(detail.bytes, m.TotalBytes)*100))
		}
	}
	return Result{
		Diagnosis: DiagnosisSlabHeavy,
		Summary:   "Kernel memory charged to the cgroup is high.",
		LikelyExplanation: "This suggests kernel-side memory such as slab allocations, dentries/inodes, socket buffers, " +
			"or other kernel memory is a meaningful part of the charge.",
		SuggestedChecks: []string{
			"Check socket-heavy workloads and connection churn.",
			"Look for many small files or directory traversal patterns.",
			"Compare with node-level slab and kernel memory metrics.",
		},
		Signals: signals,
	}
}

func mixed(m model.MemoryBreakdown) Result {
	return Result{
		Diagnosis: DiagnosisMixed,
		Summary:   "Multiple memory buckets are elevated and no single signal dominates.",
		LikelyExplanation: "The available cgroup stats suggest a mixed memory profile. Start with the largest buckets, " +
			"then compare the timing of those signals with workload activity.",
		SuggestedChecks: []string{
			"Compare RSS/anon, file cache, shmem, and slab over the same incident window.",
			"Check whether workload activity changed at the same time as memory usage.",
			"Use more specific runtime or storage tooling for the largest bucket.",
		},
		Signals: []string{
			fmt.Sprintf("RSS/anon %.0f%%, file cache %.0f%%, shmem %.0f%%, slab %.0f%%",
				m.AnonRatio()*100,
				m.FileCacheRatio()*100,
				m.ShmemRatio()*100,
				m.SlabRatio()*100,
			),
		},
	}
}

func normal(m model.MemoryBreakdown) Result {
	return Result{
		Diagnosis: DiagnosisNormal,
		Summary:   "No single high-risk memory bucket stands out from the available cgroup stats.",
		LikelyExplanation: "Based on available cgroup stats, this looks within the expected range for the sample. " +
			"Continue checking trends and limits if the pod is still close to eviction or OOM thresholds.",
		SuggestedChecks: []string{
			"Compare current memory with recent baseline and configured limits.",
			"Check application-level metrics if users still report symptoms.",
		},
		Signals: []string{
			"no dominant cgroup memory bucket detected",
		},
	}
}

func countTrue(values ...bool) int {
	count := 0
	for _, value := range values {
		if value {
			count++
		}
	}
	return count
}

func componentRatio(part, total uint64) float64 {
	if total == 0 {
		return 0
	}
	return float64(part) / float64(total)
}

func minUint64(a, b uint64) uint64 {
	if a < b {
		return a
	}
	return b
}

const mib uint64 = 1024 * 1024
