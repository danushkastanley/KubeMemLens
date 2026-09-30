// Package sessionview renders validated incident timelines without raw payloads.
package sessionview

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/incidentsession"
)

func SanitisedLines(d incidentsession.SanitisedExport) []string {
	lines := []string{fmt.Sprintf("Recorded session %s · opened %s · expires %s", d.Alias, d.OpenedAt.Format(time.RFC3339), d.ExpiresAt.Format(time.RFC3339))}
	for _, entry := range d.Entries {
		lines = append(lines, fmt.Sprintf("%d  %s  %s  %s", entry.Sequence, entry.RecordedAt.Format(time.RFC3339), entry.Kind, entry.Source))
		if entry.GapReason != "" {
			lines = append(lines, "   Gap: "+strings.ReplaceAll(entry.GapReason, "-", " "))
		}
		if entry.ClockUncertain {
			lines = append(lines, "   Clock uncertain")
		}
		for _, ref := range entry.References {
			lines = append(lines, fmt.Sprintf("   %s  schema %d  %s", ref.Alias, ref.SchemaVersion, ref.ObservedAt.Format(time.RFC3339)))
		}
		if ref := entry.TraceReference; ref != nil {
			lines = append(lines, fmt.Sprintf("   %s  %s trace report schema %d  %s", ref.Alias, ref.TraceKind, ref.ReportSchemaVersion, ref.Provenance),
				fmt.Sprintf("   %s · termination %s · coverage %s · loss %s · cleanup %s", ref.State, ref.Termination, ref.Coverage, ref.Loss, ref.Cleanup),
				"   Report body stays outside the collector; source and original target are unverified.")
		}
	}
	if d.LimitReached {
		lines = append(lines, "Session limit reached; retained evidence was not truncated.")
	}
	return lines
}

func AuthorisedLines(d incidentsession.AuthorisedExport) ([]string, error) {
	public, err := incidentsession.Sanitise(d)
	if err != nil {
		return nil, err
	}
	lines := SanitisedLines(public)
	lines = append(lines, "Private annotations:")
	for _, entry := range d.Entries {
		if entry.Note != "" {
			lines = append(lines, fmt.Sprintf("%d  %s", entry.Sequence, strconv.Quote(entry.Note)))
		}
	}
	return lines, nil
}
