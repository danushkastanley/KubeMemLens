package metrics

import (
	"errors"
	"strings"
	"testing"
)

func TestOperationalMetricsKeepHeapAndRetentionSeparate(t *testing.T) {
	heap := uint64(12345)
	r := newRenderer(8192)
	r.operational(Operational{HeapObjectsBytes: &heap, UsageEnabled: true, UsageEntries: 2, UsageBytes: 100, UsageCommits: 4, Health: &HealthOperational{Enabled: true, Entries: 3, Bytes: 200, PayloadWrites: 5, Reads: 6, Failures: 1, Throttled: 2}})
	text, err := r.String()
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{"kubememlens_collector_heap_objects_bytes 12345", "kubememlens_volume_usage_retained_bytes 100", "kubememlens_volume_health_retained_bytes 200", "kubememlens_volume_usage_commits_total 4", "kubememlens_volume_health_payload_writes_total 5"} {
		if !strings.Contains(text, line+"\n") {
			t.Fatal("missing metric", line)
		}
	}
	if strings.Contains(text, "{") || !strings.HasSuffix(text, "# EOF\n") {
		t.Fatal("dynamic labels or invalid metrics framing")
	}
	r = newRenderer(8192)
	r.operational(Operational{})
	text, err = r.String()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(text, "collector_heap_objects_bytes") || strings.Contains(text, "volume_health_entries") {
		t.Fatal("missing diagnostics became measured zero")
	}
	if !strings.Contains(text, "kubememlens_volume_usage_enabled 0") {
		t.Fatal("disabled collection not labelled")
	}
	r = newRenderer(32)
	r.operational(Operational{HeapObjectsBytes: &heap})
	if text, err = r.String(); !errors.Is(err, ErrOutputTooLarge) || text != "" {
		t.Fatal("bounded diagnostics returned partial content")
	}
}
