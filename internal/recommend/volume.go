package recommend

import "github.com/danushkastanley/kube-memlens/internal/explain"

func ForVolumeEvidence(memory *explain.Result, volumes []explain.VolumeResult) []Recommendation {
	var items []Recommendation
	if memory != nil {
		items = append(items, ForFinding(*memory)...)
	}
	seen := map[string]bool{}
	for _, item := range items {
		seen[item.ID] = true
	}
	for _, result := range volumes {
		for _, item := range ForVolumes(result) {
			if !seen[item.ID] {
				seen[item.ID] = true
				items = append(items, item)
			}
		}
	}
	return items
}

func ForVolumes(result explain.VolumeResult) []Recommendation {
	kinds := map[explain.VolumeSignalKind]bool{}
	for _, signal := range result.Signals {
		kinds[signal.Kind] = true
	}
	var items []Recommendation
	if kinds[explain.VolumeFilesystemPressure] || kinds[explain.VolumeInodePressure] {
		items = append(items, Recommendation{ID: "inspect-filesystem-headroom", Priority: "investigate",
			Action:     "Inspect filesystem free space and inode availability for the authorised volume.",
			Rationale:  "Limited filesystem headroom can affect workload writes, but filesystem capacity is not memory charge.",
			Conditions: []string{"Confirm a fresh source timestamp and whether other Pods share the claim.", "Do not resize, delete files or repair storage solely from this observation."}})
	}
	if kinds[explain.VolumeAdverseHealth] {
		items = append(items, Recommendation{ID: "inspect-source-health", Priority: "investigate",
			Action:     "Inspect each reported health source and the driver/sidecar configuration where authorised.",
			Rationale:  "Pod, controller and backend reports can disagree and have different scopes.",
			Conditions: []string{"Distinguish current observations from retained adverse history.", "A recent API read does not prove a recent health probe or establish causality."}})
	}
	if kinds[explain.VolumeTmpfsContext] && kinds[explain.VolumeSharedMemory] {
		items = append(items, Recommendation{ID: "inspect-tmpfs-attribution", Priority: "investigate",
			Action:     "Inspect memory-backed emptyDir configuration and application shared-memory use.",
			Rationale:  "Shmem includes tmpfs and shared memory; it does not attribute bytes to the named mount.",
			Conditions: []string{"Verify application ownership before considering size limits or cleanup.", "A configured size limit is not measured usage."}})
	}
	if kinds[explain.VolumeWriteback] || kinds[explain.VolumeIOStall] || kinds[explain.VolumeCacheMovement] {
		items = append(items, Recommendation{ID: "inspect-storage-memory-window", Priority: "investigate",
			Action:     "Compare bounded memory history, buffered-write activity and the separately reported storage evidence.",
			Rationale:  "Cache movement, writeback and I/O stalls can support an investigation without identifying a faulty volume.",
			Conditions: []string{"Use matching instances and aligned timestamps; do not add container PSI percentages.", "Retain missing and contradictory reports; no automatic resource or storage mutation is suggested."}})
	}
	return items
}
