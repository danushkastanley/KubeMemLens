package nodestats

import (
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/nodecontext"
)

var metricReasons = [...]nodecontext.Reason{"", nodecontext.Unsupported, nodecontext.InvalidTarget,
	nodecontext.UntrustedTLS, nodecontext.Authentication, nodecontext.Forbidden,
	nodecontext.TimedOut, nodecontext.Unreachable, nodecontext.InvalidResponse,
	nodecontext.ResponseTooLarge, nodecontext.Throttled, nodecontext.SourceUnavailable}

type Telemetry struct {
	mu       sync.RWMutex
	counts   [len(metricReasons)]uint64
	duration time.Duration
	bytes    int
}

func (t *Telemetry) record(err error, duration time.Duration, bytes int) {
	reason := nodecontext.Reason("")
	if err != nil {
		reason = reasonOf(err)
	}
	index := slices.Index(metricReasons[:], reason)
	if index < 0 {
		index = slices.Index(metricReasons[:], nodecontext.InvalidResponse)
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.counts[index]++
	t.duration, t.bytes = duration, bytes
}

// Render contains no target names, identities, paths or raw error messages.
func (t *Telemetry) Render() string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	var out strings.Builder
	out.WriteString("# HELP kubememlens_node_context_reads_total Node-context acquisition attempts including Node metadata.\n# TYPE kubememlens_node_context_reads_total counter\n")
	for index, reason := range metricReasons {
		label := string(reason)
		if label == "" {
			label = "success"
		}
		fmt.Fprintf(&out, "kubememlens_node_context_reads_total{result=%q} %d\n", label, t.counts[index])
	}
	out.WriteString("# HELP kubememlens_node_context_last_read_seconds Duration of the latest acquisition attempt.\n# TYPE kubememlens_node_context_last_read_seconds gauge\n")
	fmt.Fprintf(&out, "kubememlens_node_context_last_read_seconds %g\n", t.duration.Seconds())
	out.WriteString("# HELP kubememlens_node_context_last_response_bytes Bytes read from the latest Summary response body.\n# TYPE kubememlens_node_context_last_response_bytes gauge\n")
	fmt.Fprintf(&out, "kubememlens_node_context_last_response_bytes %d\n# EOF\n", t.bytes)
	return out.String()
}
