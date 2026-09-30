package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/incident"
	"github.com/danushkastanley/kube-memlens/internal/incidentsession"
	"github.com/spf13/cobra"
)

func (c *incidentCommands) compare() *cobra.Command {
	return &cobra.Command{Use: "compare <id> <before> <after>", Short: "Compare retained evidence aliases or digests; requires full export permission", Args: cobra.ExactArgs(3), RunE: func(cmd *cobra.Command, args []string) error {
		connection, err := c.connect()
		if err != nil {
			return err
		}
		defer connection.Close()
		data, err := connection.ExportIncident(cmd.Context(), args[0], incidentsession.ExportAuthorised)
		if err != nil {
			return err
		}
		doc, err := incidentsession.DecodeExport(data)
		if err != nil {
			return err
		}
		before, err := incidentsession.SelectCapture(*doc.Authorised, args[1])
		if err != nil {
			return err
		}
		after, err := incidentsession.SelectCapture(*doc.Authorised, args[2])
		if err != nil {
			return err
		}
		report, err := renderSessionComparison(before, after)
		if err != nil {
			return err
		}
		status, err := connection.CompareIncident(cmd.Context(), args[0], before.Digest, after.Digest)
		if err != nil {
			return err
		}
		if err := printIncidentStatus(cmd.OutOrStdout(), status); err != nil {
			return err
		}
		_, err = fmt.Fprint(cmd.OutOrStdout(), report)
		return err
	}}
}

func renderSessionComparison(before, after incidentsession.CapturedEvidence) (string, error) {
	var output bytes.Buffer
	if before.SchemaVersion == 6 && after.SchemaVersion == 6 {
		var left, right incident.HistoryBundle
		if json.Unmarshal(before.Data, &left) != nil || json.Unmarshal(after.Data, &right) != nil {
			return "", incidentsession.ErrInvalid
		}
		if right.CapturedAt.Before(left.CapturedAt) || right.Context.Changes.ObservedAt.Before(left.Context.Changes.ObservedAt) {
			fmt.Fprintln(&output, "Capture clocks are not ordered; observations are shown separately.")
			if err := replayHistory(&output, left, "", ""); err != nil {
				return "", err
			}
			if err := replayHistory(&output, right, "", ""); err != nil {
				return "", err
			}
		} else if err := compareHistoryDocuments(&output, incident.Document{History: &left}, incident.Document{History: &right}, "", "", ""); err != nil {
			return "", err
		}
	} else {
		if before.SchemaVersion < 1 || before.SchemaVersion > 2 || after.SchemaVersion < 1 || after.SchemaVersion > 2 {
			return "", fmt.Errorf("comparison requires captures from the same memory domain")
		}
		var left, right api.IncidentBundle
		if json.Unmarshal(before.Data, &left) != nil || json.Unmarshal(after.Data, &right) != nil || len(left.Pods) != 1 || len(right.Pods) != 1 {
			return "", incidentsession.ErrInvalid
		}
		a, b := left.Pods[0], right.Pods[0]
		elapsed := b.CapturedAt.Sub(a.CapturedAt)
		if a.Namespace != b.Namespace || a.PodName != b.PodName || a.PodUID != b.PodUID || a.NodeName != b.NodeName {
			fmt.Fprintln(&output, "Different Pod instances or selections; values describe separate observations.")
			elapsed = 0
		}
		if elapsed <= 0 {
			fmt.Fprintln(&output, "No forward source interval is established; no change rate is inferred.")
		}
		fmt.Fprintf(&output, "Before evidence: freshness %q, completeness %q\nAfter evidence: freshness %q, completeness %q\n", a.Freshness, a.Completeness, b.Freshness, b.Completeness)
		for i := range a.Containers {
			a.Containers[i].ContainerName = strconv.Quote(a.Containers[i].ContainerName)
		}
		for i := range b.Containers {
			b.Containers[i].ContainerName = strconv.Quote(b.Containers[i].ContainerName)
		}
		printPodComparison(&output, "Retained Pod memory comparison", a, b, elapsed)
	}
	if output.Len() > 128<<10 {
		return "", incidentsession.ErrCapacity
	}
	// Preserve generated line layout while quoting terminal controls and hidden
	// formatting characters from any source-provided display text.
	var safe strings.Builder
	for _, r := range output.String() {
		if r != '\n' && r != '\t' && (unicode.IsControl(r) || unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp)) {
			escaped := strconv.QuoteRune(r)
			safe.WriteString(escaped[1 : len(escaped)-1])
		} else {
			safe.WriteRune(r)
		}
	}
	if safe.Len() > 128<<10 {
		return "", incidentsession.ErrCapacity
	}
	return safe.String(), nil
}
