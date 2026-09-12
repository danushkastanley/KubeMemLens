package explain

import (
	"fmt"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/model"
)

// Maxima describe the most affected observed container, never summed PSI or
// Pod-wide PSI. Missing or stale containers remain visible in coverage counts.
type VolumeIO struct {
	Containers   int       `json:"containers"`
	Available    int       `json:"available"`
	Unreported   int       `json:"unreported"`
	Unavailable  int       `json:"unavailable"`
	Invalid      int       `json:"invalid"`
	Stale        int       `json:"stale"`
	SomeMax10    float64   `json:"someMax10"`
	FullMax10    float64   `json:"fullMax10"`
	Resets       int       `json:"resets"`
	OldestSample time.Time `json:"oldestSample,omitzero"`
	NewestSample time.Time `json:"newestSample,omitzero"`
}

func volumeIO(pod api.PodSnapshot, now time.Time) VolumeIO {
	r := VolumeIO{Containers: len(pod.Containers)}
	for _, c := range pod.Containers {
		p := c.Memory.IOPressure
		if p.Validate() != nil {
			r.Invalid++
			continue
		}
		switch p.State {
		case "", model.IOUnreported:
			r.Unreported++
		case model.IOUnavailable:
			r.Unavailable++
		case model.IOInvalid:
			r.Invalid++
		case model.IOAvailable:
			if c.Freshness == api.EvidenceFreshnessStale || !recentVolumeTime(c.CapturedAt, now, VolumeMemoryMaxAge) {
				r.Stale++
				continue
			}
			r.Available++
			if r.OldestSample.IsZero() || c.CapturedAt.Before(r.OldestSample) {
				r.OldestSample = c.CapturedAt
			}
			if c.CapturedAt.After(r.NewestSample) {
				r.NewestSample = c.CapturedAt
			}
			r.SomeMax10 = max(r.SomeMax10, p.Some.Avg10)
			r.FullMax10 = max(r.FullMax10, p.Full.Avg10)
			if p.CounterReset {
				r.Resets++
			}
		}
	}
	return r
}

func (r *VolumeResult) memorySignals(input VolumeInput, fresh bool) bool {
	pod := input.Pod
	if r.IO.Available != r.IO.Containers || r.IO.Containers == 0 {
		r.caveat("I/O pressure coverage is incomplete; unavailable containers are not zero-stall measurements.")
	}
	if r.IO.Resets > 0 {
		r.caveat("I/O cumulative counters reset; the affected delta window is unknown.")
	}
	if !fresh {
		return false
	}
	support := false
	if pod.Memory.FileCacheRatio() >= .4 {
		r.add(VolumeCacheContext, -1, "cgroup-v2/memory.stat", "File cache is a substantial part of memory charge; current size alone does not establish growth or a storage fault.", pod.CapturedAt, false)
	}
	if pod.Memory.ShmemRatio() >= .3 {
		r.add(VolumeSharedMemory, -1, "cgroup-v2/memory.stat", "Shared memory/tmpfs is a substantial part of memory charge; configuration does not attribute it to a named volume.", pod.CapturedAt, false)
	}
	if pod.Memory.DirtyWritebackRatio() >= .1 || pod.Memory.DirtyWritebackBytes() >= 256*1024*1024 {
		r.add(VolumeWriteback, -1, "cgroup-v2/memory.stat", "Dirty/writeback pages are elevated; buffered writes are a plausible contributor, not proof of slow storage.", pod.CapturedAt, false)
		support = true
	}
	if r.IO.SomeMax10 >= 1 || r.IO.FullMax10 > 0 {
		r.add(VolumeIOStall, -1, model.IOPressureSource, fmt.Sprintf("I/O stalls observed in container cgroups; highest some/full avg10 %.2f%%/%.2f%% (%d/%d containers available).", r.IO.SomeMax10, r.IO.FullMax10, r.IO.Available, r.IO.Containers), pod.CapturedAt, false)
		r.caveat("Container I/O PSI cannot attribute stalls to a particular volume; percentages are not summed.")
		support = true
	}
	before := input.Previous
	if !comparableVolumeMemory(before, pod) {
		r.caveat("Cache movement requires two ordered samples from the same Pod and container instances; a single sample cannot establish growth.")
		return support
	}
	if before.Memory.CacheBytes() != pod.Memory.CacheBytes() {
		r.add(VolumeCacheMovement, -1, "cgroup-v2/memory.stat", fmt.Sprintf("File cache changed from %s to %s across the observed window.", model.FormatCompactBytes(before.Memory.CacheBytes()), model.FormatCompactBytes(pod.Memory.CacheBytes())), pod.CapturedAt, false)
		support = true
	}
	return support
}

func comparableVolumeMemory(before *api.PodSnapshot, after api.PodSnapshot) bool {
	if before == nil || before.PodUID == "" || before.PodUID != after.PodUID || before.Namespace != after.Namespace || before.PodName != after.PodName || before.NodeName != after.NodeName ||
		before.CapturedAt.IsZero() || !before.CapturedAt.Before(after.CapturedAt) || after.CapturedAt.Sub(before.CapturedAt) > 2*time.Minute || before.Freshness == api.EvidenceFreshnessStale || len(before.Containers) != len(after.Containers) || len(after.Containers) == 0 {
		return false
	}
	instances := map[string]string{}
	for _, c := range before.Containers {
		if c.ContainerID == "" {
			return false
		}
		instances[c.ContainerName] = c.ContainerID
	}
	for _, c := range after.Containers {
		if c.ContainerID == "" || instances[c.ContainerName] != c.ContainerID {
			return false
		}
	}
	return true
}
