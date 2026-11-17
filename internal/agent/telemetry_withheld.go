package agent

import (
	"fmt"
	"slices"
	"strings"
)

// WithheldReason says why a successful scan was not published. A withheld
// snapshot is neither a failed scan nor a failed post, so without its own
// counter an agent that publishes nothing looks healthy.
type WithheldReason string

const (
	WithheldMetadataCacheUnsynchronised WithheldReason = "metadata_cache_unsynchronised"
	WithheldPartialScan                 WithheldReason = "partial_scan_not_published"
)

// withheldReasons is the complete label set, so the metric's cardinality is fixed.
var withheldReasons = [...]WithheldReason{WithheldMetadataCacheUnsynchronised, WithheldPartialScan}

func (t *Telemetry) RecordWithheld(reason WithheldReason) {
	index := slices.Index(withheldReasons[:], reason)
	if index < 0 {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.withheld[index]++
}

func (t *Telemetry) writeWithheldLocked(b *strings.Builder) {
	writeMetricHeader(b, "kubememlens_agent_snapshots_withheld_total", "KubeMemLens agent snapshots from successful scans that were not published, by reason.", "counter")
	for index, reason := range withheldReasons {
		fmt.Fprintf(b, "kubememlens_agent_snapshots_withheld_total{reason=%q} %d\n", reason, t.withheld[index])
	}
}
