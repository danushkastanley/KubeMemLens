package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/danushkastanley/kube-memlens/internal/historyview"
	"github.com/danushkastanley/kube-memlens/internal/incident"
	"github.com/danushkastanley/kube-memlens/internal/memoryhistory"
)

func replayHistory(w io.Writer, b incident.HistoryBundle, pod, node string) error {
	request := b.Context.History.Selection.Request
	if node != "" || (pod != "" && (request.Scope == memoryhistory.Workload || pod != request.Namespace+"/"+request.Name)) {
		return fmt.Errorf("the selected target is not present in this history capture")
	}
	fmt.Fprintf(w, "History captured: %s · redacted=%t\n", b.CapturedAt.Format("2006-01-02 15:04:05 UTC"), b.Redacted)
	for _, caveat := range b.Caveats {
		fmt.Fprintln(w, caveat)
	}
	_, err := fmt.Fprintln(w, strings.Join(historyview.ContextLines(b.Context, b.CapturedAt, 100), "\n"))
	return err
}

func compareHistoryDocuments(w io.Writer, before, after incident.Document, pod, workload, node string) error {
	if before.History == nil || after.History == nil || pod != "" || workload != "" || node != "" {
		return fmt.Errorf("history comparison requires two schema-6 captures and --trends, without another target selector")
	}
	result, err := incident.CompareHistory(*before.History, *after.History)
	if err != nil {
		return err
	}
	fmt.Fprintln(w, "History comparison · identity:", result.Continuity)
	if result.Continuity == "identity-unavailable" {
		fmt.Fprintln(w, "Capture-local aliases cannot establish continuity across files; no replacement is inferred.")
	}
	if !result.ComparableMetric {
		fmt.Fprintln(w, "Sources or metrics differ; memory values are not directly comparable.")
	}
	fmt.Fprintln(w, "Before:")
	if err := replayHistory(w, *before.History, "", ""); err != nil {
		return err
	}
	fmt.Fprintln(w, "After:")
	if err := replayHistory(w, *after.History, "", ""); err != nil {
		return err
	}
	for _, marker := range result.Replacements {
		fmt.Fprintf(w, "Pod %s changed instance between %s and %s; exact replacement time is unknown. Memory series remain separate.\n", marker.Subject.Name, marker.At.Format("2006-01-02 15:04:05 UTC"), marker.Until.Format("2006-01-02 15:04:05 UTC"))
	}
	return nil
}
