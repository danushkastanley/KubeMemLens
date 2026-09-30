package tui

import (
	"fmt"

	"github.com/danushkastanley/kube-memlens/internal/traceclient"
	"github.com/danushkastanley/kube-memlens/internal/traceview"
)

func (m appModel) tracePanelLines() []string {
	p := m.tracePanel
	lines := []string{"Bounded trace · development profile", "Resource qualification incomplete. Trace access is separately authorised."}
	if p.revoked {
		return append(lines, "Access changed. Evidence cleared; cancellation requested.", "Cleanup: "+string(p.snapshot.Cleanup))
	}
	state := string(p.snapshot.State)
	if p.session == nil {
		state = "configure"
	}
	if p.loading {
		state = "preflight"
	}
	lines = append(lines, fmt.Sprintf("Target: %s/%s · %s", p.target.Namespace, p.target.PodName, p.target.ContainerName), fmt.Sprintf("Type: %s · duration: %s · event limit: %d", p.intent.Kind, p.intent.Bounds.Duration, p.intent.Bounds.Events), "State: "+state)
	if p.session == nil && !p.loading {
		lines = append(lines, "f files · c cache · o OOM · d duration · l event limit", "Enter checks this exact container lifetime.")
	}
	if p.snapshot.State == traceclient.StateReady {
		lines = append(lines, "Preflight passed. Review the target and limits above.", "Enter starts one bounded trace; s cancels before admission.")
	}
	if p.session != nil {
		lines = append(lines, fmt.Sprintf("Cleanup: %s · transport complete: %t", p.snapshot.Cleanup, p.snapshot.Result.TransportComplete), fmt.Sprintf("Validated event frames: %d", p.snapshot.Result.DeliveredEvents))
		if summary, ok := p.snapshot.Result.Summary(); ok {
			lines = append(lines, traceview.SummaryLines(summary)...)
		} else {
			lines = append(lines, "Engine counts and observation windows remain unreported.")
		}
	}
	for _, err := range []error{p.err, p.snapshot.Failure, p.snapshot.CleanupFailure} {
		if err != nil {
			lines = append(lines, err.Error())
		}
	}
	lines = append(lines, "No raw events, paths or process details are retained.")
	switch p.exportMode {
	case "path":
		lines = append(lines, "Export destination: "+p.exportPath, "Enter reviews export · Esc cancels")
	case "confirm":
		lines = append(lines, "Sensitive operational timings and counters will be written.", "Target aliases replace identities; caveats and provenance remain.", "Press y to confirm this export · Esc cancels")
	case "writing":
		lines = append(lines, "Writing private report…")
	case "saved":
		lines = append(lines, "Report saved. Esc returns to trace controls.")
	default:
		if p.snapshot.State.Terminal() {
			lines = append(lines, "x export · n select current container for a new trace")
		} else {
			lines = append(lines, "s cancel trace")
		}
		lines = append(lines, "Esc returns to navigation; tracing continues · T reopens")
	}
	return lines
}
func (m appModel) renderTracePanel(width int) string {
	lines := wrapText(m.tracePanelLines(), width)
	v := m.tracePanel.viewport
	v.resize(max(1, m.bodyRows()-1))
	v.reconcile(len(lines))
	return truncateLines(viewportWindow(v, lines), width)
}
