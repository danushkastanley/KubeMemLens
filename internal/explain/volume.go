package explain

import (
	"fmt"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/volumecontext"
	"github.com/danushkastanley/kube-memlens/internal/volumehealth"
)

const VolumeMemoryMaxSkew = 30 * time.Second
const VolumeMemoryMaxAge = 45 * time.Second

type VolumeSignalKind string

const (
	VolumeFilesystemPressure VolumeSignalKind = "filesystem-pressure"
	VolumeInodePressure      VolumeSignalKind = "inode-pressure"
	VolumeAdverseHealth      VolumeSignalKind = "adverse-health"
	VolumeTmpfsContext       VolumeSignalKind = "tmpfs-context"
	VolumeWriteback          VolumeSignalKind = "dirty-writeback"
	VolumeCacheMovement      VolumeSignalKind = "cache-movement"
	VolumeCacheContext       VolumeSignalKind = "cache-context"
	VolumeSharedMemory       VolumeSignalKind = "shared-memory"
	VolumeIOStall            VolumeSignalKind = "io-stall"
	VolumeAlignedEvidence    VolumeSignalKind = "aligned-storage-memory"
)

// VolumeIndex is local to this response (-1 means Pod/container evidence).
// It is never an identity for matching independent captures.
type VolumeSignal struct {
	Kind        VolumeSignalKind `json:"kind"`
	VolumeIndex int              `json:"volumeIndex"`
	Source      string           `json:"source"`
	Summary     string           `json:"summary"`
	ObservedAt  time.Time        `json:"observedAt"`
	Historical  bool             `json:"historical"`
}

type VolumeResult struct {
	MemorySeverity  Severity       `json:"memorySeverity"`
	StorageSeverity Severity       `json:"storageSeverity"`
	Signals         []VolumeSignal `json:"signals"`
	Caveats         []string       `json:"caveats"`
	IO              VolumeIO       `json:"io"`
}

// The caller supplies one authorised server join and an instance-checked Pod.
// This module performs no Kubernetes joins and emits no identity or backend text.
type VolumeInput struct {
	Pod      api.PodSnapshot
	Volumes  volumecontext.View
	Previous *api.PodSnapshot
	Now      time.Time
}

func AnalyzeVolumes(input VolumeInput) VolumeResult {
	r := VolumeResult{MemorySeverity: AnalyzePodAt(input.Pod, input.Now).Severity, StorageSeverity: SeverityInfo,
		Caveats: []string{"Correlation does not prove causality. Filesystem bytes are separate from cgroup memory charge."}}
	if input.Now.IsZero() || input.Volumes.Namespace != input.Pod.Namespace || input.Volumes.PodName != input.Pod.PodName || len(input.Volumes.Volumes) > volumecontext.MaxVolumesPerPod || len(input.Pod.Containers) > 256 {
		r.Caveats = append(r.Caveats, "Volume and memory scope is unavailable or exceeds the bounded analysis limit.")
		return r
	}
	agedView, err := volumecontext.AgeView(input.Volumes, input.Now)
	if err != nil {
		r.Caveats = append(r.Caveats, "Volume context is invalid or expired; cross-source analysis is unavailable.")
		return r
	}
	input.Volumes = agedView
	freshMemory := input.Pod.Freshness != api.EvidenceFreshnessStale && recentVolumeTime(input.Pod.CapturedAt, input.Now, VolumeMemoryMaxAge)
	if !freshMemory {
		r.Caveats = append(r.Caveats, "Memory is stale or has an unknown sample time; cross-source correlation is withheld.")
	}
	r.IO = volumeIO(input.Pod, input.Now)
	memorySupport := r.memorySignals(input, freshMemory)
	for index, volume := range input.Volumes.Volumes {
		if volume.Configuration.MemoryBacked {
			r.add(VolumeTmpfsContext, index, "kubernetes-pod-spec", "Memory-backed emptyDir is configured; its size limit is configuration, not measured charge.", time.Time{}, false)
			if input.Pod.Memory.ShmemBytes > 0 {
				r.caveat("Shmem includes shared memory and tmpfs; it cannot identify which mounted volume owns the charge.")
			}
		}
		usage := volume.Usage
		fs, historical := usage.Filesystem, false
		if fs == nil && usage.LastGood != nil {
			fs, historical = usage.LastGood, true
		}
		if fs == nil {
			r.caveat(fmt.Sprintf("Filesystem evidence: %s (%s). Missing values are not zero.", usage.Availability, usage.Reason))
		} else {
			aged := historical || usage.Freshness != volumehealth.Fresh || !recentVolumeTime(fs.CapturedAt, input.Now, volumecontext.StaleAfter)
			pressure := r.filesystemSignals(index, *fs, aged)
			if aged {
				r.caveat("Aged filesystem values are historical evidence and do not establish current saturation.")
			}
			if pressure && memorySupport && freshMemory && !aged {
				r.correlate(index, "kubelet-summary", fs.CapturedAt, input.Pod.CapturedAt)
			}
		}
		for _, health := range volume.Health {
			r.healthSignal(input, index, health.Observation, false, memorySupport && freshMemory)
			if health.LastGood != nil {
				r.healthSignal(input, index, health.LastGood.Observation, true, false)
			}
		}
	}
	return r
}

func (r *VolumeResult) add(kind VolumeSignalKind, index int, source, summary string, at time.Time, historical bool) {
	r.Signals = append(r.Signals, VolumeSignal{kind, index, source, summary, at, historical})
}

func (r *VolumeResult) caveat(text string) {
	for _, existing := range r.Caveats {
		if existing == text {
			return
		}
	}
	r.Caveats = append(r.Caveats, text)
}

func recentVolumeTime(at, now time.Time, age time.Duration) bool {
	return !at.IsZero() && !at.After(now.Add(volumecontext.FutureSkew)) && now.Sub(at) <= age
}

func (r *VolumeResult) correlate(index int, source string, at, memoryAt time.Time) {
	skew := at.Sub(memoryAt)
	if skew < -VolumeMemoryMaxSkew || skew > VolumeMemoryMaxSkew {
		r.caveat("Storage and memory sample times differ by more than 30 seconds; cross-source correlation is withheld.")
		return
	}
	r.add(VolumeAlignedEvidence, index, source+" + cgroup-v2", "Storage evidence overlaps the memory observation window; inspect the write/cache path before considering a resource change.", at, false)
}

func (r *VolumeResult) filesystemSignals(index int, fs volumecontext.Filesystem, historical bool) bool {
	pressure := false
	for _, field := range []struct {
		kind              VolumeSignalKind
		label             string
		used, free, total *uint64
	}{
		{VolumeFilesystemPressure, "Filesystem", fs.UsedBytes, fs.AvailableBytes, fs.CapacityBytes},
		{VolumeInodePressure, "Inodes", fs.InodesUsed, fs.InodesFree, fs.Inodes},
	} {
		if field.total == nil || *field.total == 0 {
			continue
		}
		usedHigh := field.used != nil && float64(*field.used)/float64(*field.total) >= .9
		freeLow := field.free != nil && float64(*field.free)/float64(*field.total) <= .1
		if !usedHigh && !freeLow {
			continue
		}
		pressure = true
		r.add(field.kind, index, "kubelet-summary", field.label+" has at least 90% used or at most 10% available; confirm the workload's capacity needs.", fs.CapturedAt, historical)
		if !historical {
			r.StorageSeverity = SeverityHigh
		}
	}
	return pressure
}

func (r *VolumeResult) healthSignal(input VolumeInput, index int, h volumehealth.Observation, historical, memorySupport bool) {
	if h.Availability != volumehealth.Reported && !h.Adverse {
		r.caveat(fmt.Sprintf("Health source %s: %s (%s).", h.Source, h.Availability, h.Reason))
		return
	}
	if h.UnknownStatus {
		r.caveat("An unknown future health condition is retained as adverse/unknown, not healthy.")
	}
	r.caveat("Health API observation time is not probe time; a transition timestamp is not a heartbeat.")
	if !h.Adverse {
		return
	}
	aged := historical || h.ObservationFreshness != volumehealth.Fresh || !recentVolumeTime(h.ObservedAt, input.Now, volumecontext.ExpireAfter)
	r.add(VolumeAdverseHealth, index, string(h.Source), "An adverse storage condition was reported by this source; other sources may disagree.", h.ObservedAt, aged)
	if aged {
		r.caveat("Aged adverse health is retained as historical evidence; present health is unknown.")
	} else {
		r.StorageSeverity = SeverityHigh
	}
	if h.Scope == volumehealth.BackendScope {
		r.caveat("CSINode backend health covers the Node/driver, not a specific volume or process.")
	}
	if memorySupport && !aged {
		r.correlate(index, string(h.Source), h.ObservedAt, input.Pod.CapturedAt)
	}
}
