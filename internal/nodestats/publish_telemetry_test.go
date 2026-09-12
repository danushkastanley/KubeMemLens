package nodestats

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestPublishTelemetryPreservesAttemptOutcomeWithoutErrorText(t *testing.T) {
	telemetry := &Telemetry{}
	if strings.Contains(telemetry.Render(), "last_post_seconds") {
		t.Fatal("unobserved delivery latency became zero")
	}
	telemetry.RecordPublish(20*time.Millisecond, nil)
	telemetry.RecordPublish(40*time.Millisecond, errors.New("private-pod-token-credential"))
	text := telemetry.Render()
	for _, value := range []string{"kubememlens_node_context_posts_total{result=\"success\"} 1", "kubememlens_node_context_posts_total{result=\"failure\"} 1", "kubememlens_node_context_last_post_seconds 0.04"} {
		if !strings.Contains(text, value) {
			t.Fatal("missing measured delivery", value)
		}
	}
	if strings.Contains(text, "private-pod") || strings.Count(text, "# EOF") != 1 {
		t.Fatal("unsafe telemetry framing or error disclosure")
	}
}
