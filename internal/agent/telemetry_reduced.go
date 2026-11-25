package agent

import (
	"fmt"
	"strings"
)

// RecordReducedMetadata records how many Pods in the latest scan had metadata
// left out of the snapshot to fit the collector's limits. The snapshot cannot
// say that a label, owner or runtime class was omitted, so without this gauge
// an omitted value looks the same as one the Pod never had.
func (t *Telemetry) RecordReducedMetadata(pods int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.reducedPods = pods
}

func (t *Telemetry) writeReducedMetadataLocked(b *strings.Builder) {
	writeMetricHeader(b, "kubememlens_agent_metadata_reduced_pods", "Pods in the latest KubeMemLens agent scan whose metadata was reduced to fit collector limits.", "gauge")
	fmt.Fprintf(b, "kubememlens_agent_metadata_reduced_pods %d\n", t.reducedPods)
}
