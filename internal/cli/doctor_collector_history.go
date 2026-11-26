package cli

import (
	"fmt"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
)

// The collector does not report its history window, which -history-duration
// can change, so this uses the default. A collector run with a longer window
// stops being called recent before that window is full again.
const defaultHistoryWindow = 15 * time.Minute

// addCollectorHistoryCheck says when the collector started, because a restart
// loses Pod history and event baselines (ADR 0033). It always passes: doctor
// --strict is the readiness gate after install and rollback, and a collector
// that has just started is healthy.
func (r *doctorReport) addCollectorHistoryCheck(reliability api.CollectorReliability) {
	r.addCheck("collector history", "pass", collectorHistorySummary(reliability))
}

// collectorHistorySummary compares two times from the collector's own clock,
// so a skewed client clock cannot make an old collector look new.
func collectorHistorySummary(reliability api.CollectorReliability) string {
	started := reliability.StartedAt.UTC().Format(time.RFC3339)
	retained := max(reliability.LastReceivedAt.Sub(reliability.StartedAt), 0)
	if retained >= defaultHistoryWindow {
		return "collector has run since " + started
	}
	return fmt.Sprintf("collector started at %s and holds at most %s of Pod history; nothing from before then is kept, and only an incident capture taken earlier has that evidence",
		started, retained.Round(time.Second))
}
