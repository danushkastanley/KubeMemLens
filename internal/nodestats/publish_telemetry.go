package nodestats

import (
	"fmt"
	"strings"
	"time"
)

// RecordPublish measures delivery through the production publisher, including
// transport and collector acknowledgement. Error text is never retained.
func (t *Telemetry) RecordPublish(duration time.Duration, err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.postAttempts++
	if err != nil {
		t.postFailures++
	}
	t.postDuration = duration
}

// Render holds the telemetry read lock while invoking this helper.
func (t *Telemetry) renderPosts(out *strings.Builder) {
	out.WriteString("# HELP kubememlens_node_context_posts_total Snapshot delivery attempts through the production publisher.\n# TYPE kubememlens_node_context_posts_total counter\n")
	fmt.Fprintf(out, "kubememlens_node_context_posts_total{result=\"success\"} %d\n", t.postAttempts-t.postFailures)
	fmt.Fprintf(out, "kubememlens_node_context_posts_total{result=\"failure\"} %d\n", t.postFailures)
	if t.postAttempts > 0 {
		out.WriteString("# HELP kubememlens_node_context_last_post_seconds Duration of the latest snapshot delivery attempt including transport and acknowledgement.\n# TYPE kubememlens_node_context_last_post_seconds gauge\n")
		fmt.Fprintf(out, "kubememlens_node_context_last_post_seconds %g\n", t.postDuration.Seconds())
	}
}
