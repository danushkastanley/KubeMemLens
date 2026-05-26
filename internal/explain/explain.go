package explain

import (
	"fmt"

	"github.com/danushkastanley/kube-memlens/internal/model"
)

type Diagnosis string

const (
	DiagnosisCacheHeavy          Diagnosis = "cache-heavy"
	DiagnosisRSSHeavy            Diagnosis = "rss-heavy"
	DiagnosisTmpfsHeavy          Diagnosis = "tmpfs-heavy"
	DiagnosisDirtyWritebackHeavy Diagnosis = "dirty-writeback-heavy"
	DiagnosisSlabHeavy           Diagnosis = "slab-heavy"
	DiagnosisOOMRisk             Diagnosis = "oom-risk"
	DiagnosisMixed               Diagnosis = "mixed"
	DiagnosisNormal              Diagnosis = "normal"
)

type Result struct {
	Diagnosis         Diagnosis
	Summary           string
	LikelyExplanation string
	SuggestedChecks   []string
	Signals           []string
}

func Analyze(m model.MemoryBreakdown) Result {
	if m.HasOOMRisk() {
		return oomRisk(m)
	}

	dirtyHigh := m.TotalBytes > 0 && (m.DirtyWritebackRatio() >= 0.10 || m.DirtyWritebackBytes() >= 256*mib)
	tmpfsHigh := m.TotalBytes > 0 && m.ShmemRatio() >= 0.30
	slabHigh := m.TotalBytes > 0 && m.SlabRatio() >= 0.20
	rssHigh := m.TotalBytes > 0 && m.AnonRatio() >= 0.65
	cacheHigh := m.TotalBytes > 0 && m.FileCacheRatio() >= 0.40 && m.AnonRatio() <= 0.50

	majorSignals := countTrue(dirtyHigh, tmpfsHigh, slabHigh, rssHigh, cacheHigh)
	if majorSignals > 1 && !dirtyHigh && !tmpfsHigh && !rssHigh {
		return mixed(m)
	}

	switch {
	case dirtyHigh:
		return dirtyWritebackHeavy(m)
	case tmpfsHigh:
		return tmpfsHeavy(m)
	case slabHigh:
		return slabHeavy(m)
	case rssHigh:
		return rssHeavy(m)
	case cacheHigh:
		return cacheHeavy(m)
	default:
		return normal(m)
	}
}

func oomRisk(m model.MemoryBreakdown) Result {
	signals := []string{}
	if m.OOMEvents > 0 {
		signals = append(signals, fmt.Sprintf("memory.events oom=%d", m.OOMEvents))
	}
	if m.OOMKillEvents > 0 {
		signals = append(signals, fmt.Sprintf("memory.events oom_kill=%d", m.OOMKillEvents))
	}
	if m.MaxEvents > 0 {
		signals = append(signals, fmt.Sprintf("memory.events max=%d", m.MaxEvents))
	}
	if m.HighEvents > 0 {
		signals = append(signals, fmt.Sprintf("memory.events high=%d", m.HighEvents))
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

func cacheHeavy(m model.MemoryBreakdown) Result {
	signals := []string{
		fmt.Sprintf("file cache is %.0f%% of total charged memory", m.FileCacheRatio()*100),
		fmt.Sprintf("RSS/anon is %.0f%% of total charged memory", m.AnonRatio()*100),
	}
	if m.FileBytes > 0 && float64(m.ActiveFileBytes)/float64(m.FileBytes) >= 0.60 {
		signals = append(signals, "active_file is high relative to file cache")
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
		Signals: []string{
			fmt.Sprintf("slab/kernel memory is %.0f%% of total charged memory", m.SlabRatio()*100),
		},
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

const mib uint64 = 1024 * 1024
