package explain

import (
	"fmt"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/volumecontext"
	"github.com/danushkastanley/kube-memlens/internal/volumehealth"
)

type VolumeComparisonRow struct {
	Before          int  `json:"before"`
	After           int  `json:"after"`
	UsageComparable bool `json:"usageComparable"`
	SharedClaim     bool `json:"sharedClaim"`
}
type VolumeComparison struct {
	MemoryComparable bool                  `json:"memoryComparable"`
	Rows             []VolumeComparisonRow `json:"rows"`
	UnmatchedBefore  []int                 `json:"unmatchedBefore"`
	UnmatchedAfter   []int                 `json:"unmatchedAfter"`
	Caveats          []string              `json:"caveats"`
}

func CompareVolumes(before, after VolumeInput) (VolumeComparison, error) {
	r := VolumeComparison{}
	if before.Now.IsZero() || after.Now.Before(before.Now) || volumecontext.ValidateView(before.Volumes, before.Now) != nil || volumecontext.ValidateView(after.Volumes, after.Now) != nil {
		return r, fmt.Errorf("volume comparison requires ordered captures and valid volume evidence")
	}
	if before.Pod.Namespace != before.Volumes.Namespace || before.Pod.PodName != before.Volumes.PodName || after.Pod.Namespace != after.Volumes.Namespace || after.Pod.PodName != after.Volumes.PodName {
		return r, fmt.Errorf("volume comparison scope mismatch")
	}
	r.MemoryComparable = sameVolumeMemoryInstances(before.Pod, after.Pod) && after.Pod.CapturedAt.After(before.Pod.CapturedAt) && recentVolumeTime(before.Pod.CapturedAt, before.Now, VolumeMemoryMaxAge) && recentVolumeTime(after.Pod.CapturedAt, after.Now, VolumeMemoryMaxAge) && before.Pod.Freshness != "stale" && after.Pod.Freshness != "stale"
	if !r.MemoryComparable {
		r.Caveats = append(r.Caveats, "Memory values are separate observations; missing identity, a changed instance, stale data or unordered sample times prevent deltas.")
	}
	used := map[int]bool{}
	matchedAfter := map[int]bool{}
	for ai, a := range after.Volumes.Volumes {
		for bi, b := range before.Volumes.Volumes {
			if used[bi] {
				continue
			}
			identity := a.EvidenceID != "" && a.EvidenceID == b.EvidenceID
			shared := !identity && a.FilesystemID != "" && a.FilesystemID == b.FilesystemID
			if !identity && !shared {
				continue
			}
			used[bi] = true
			matchedAfter[ai] = true
			bf, af := b.Usage.Filesystem, a.Usage.Filesystem
			comparable := b.Usage.Availability == volumehealth.Reported && a.Usage.Availability == volumehealth.Reported && b.Usage.Freshness == volumehealth.Fresh && a.Usage.Freshness == volumehealth.Fresh && bf != nil && af != nil && af.CapturedAt.After(bf.CapturedAt) && recentVolumeTime(bf.CapturedAt, before.Now, volumecontext.StaleAfter) && recentVolumeTime(af.CapturedAt, after.Now, volumecontext.StaleAfter)
			r.Rows = append(r.Rows, VolumeComparisonRow{Before: bi, After: ai, UsageComparable: comparable, SharedClaim: shared})
			break
		}
	}
	for i := range before.Volumes.Volumes {
		if !used[i] {
			r.UnmatchedBefore = append(r.UnmatchedBefore, i)
		}
	}
	for i := range after.Volumes.Volumes {
		if !matchedAfter[i] {
			r.UnmatchedAfter = append(r.UnmatchedAfter, i)
		}
	}
	if len(r.Rows) == 0 {
		r.Caveats = append(r.Caveats, "No volume binding continuity is established. Names and capture-local aliases are not identities; filesystem deltas are withheld.")
	}
	if len(r.UnmatchedBefore)+len(r.UnmatchedAfter) > 0 {
		r.Caveats = append(r.Caveats, "Unlinked volume observations remain separate; absence of a match does not establish creation or deletion.")
	}
	r.Caveats = append(r.Caveats, "Binding references do not prove that a filesystem was never remounted or reformatted. Shared-claim readings are not per-Pod usage.", "Point differences do not establish sustained growth or causality; filesystem bytes remain outside memory totals.")
	return r, nil
}

func sameVolumeMemoryInstances(before, after api.PodSnapshot) bool {
	if before.PodUID == "" || before.PodUID != after.PodUID || before.Namespace != after.Namespace || before.PodName != after.PodName || before.NodeName != after.NodeName || len(before.Containers) == 0 || len(before.Containers) != len(after.Containers) {
		return false
	}
	ids := map[string]string{}
	for _, c := range before.Containers {
		if c.ContainerID == "" || ids[c.ContainerName] != "" {
			return false
		}
		ids[c.ContainerName] = c.ContainerID
	}
	for _, c := range after.Containers {
		if c.ContainerID == "" || ids[c.ContainerName] != c.ContainerID {
			return false
		}
		delete(ids, c.ContainerName)
	}
	return len(ids) == 0
}
