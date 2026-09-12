// Package nodeview formats Node evidence without performing memory accounting.
package nodeview

import (
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/charmbracelet/x/ansi"
	"github.com/danushkastanley/kube-memlens/internal/model"
	"github.com/danushkastanley/kube-memlens/internal/nodecontext"
)

func bytes(value *uint64) string {
	if value == nil {
		return "unreported"
	}
	return model.FormatBytes(*value)
}

func count(value *uint64) string {
	if value == nil {
		return "unreported"
	}
	return fmt.Sprintf("%d", *value)
}

func instant(at time.Time) string {
	if at.IsZero() {
		return "unreported"
	}
	return at.UTC().Format(time.RFC3339Nano)
}

func sample(at, now time.Time) string {
	if at.IsZero() {
		return "unreported"
	}
	return fmt.Sprintf("%s (age %s)", instant(at), now.Sub(at).Round(time.Second))
}

func memoryLines(label string, memory *nodecontext.Memory, now time.Time) []string {
	if memory == nil {
		return []string{label + ": unreported"}
	}
	lines := []string{label + " sampled: " + sample(memory.CapturedAt, now),
		"Usage: " + bytes(memory.UsageBytes), "Available: " + bytes(memory.AvailableBytes),
		"Working set: " + bytes(memory.WorkingSetBytes), "RSS: " + bytes(memory.RSSBytes),
		"Page faults (cumulative): " + count(memory.PageFaults), "Major page faults (cumulative): " + count(memory.MajorPageFaults)}
	if memory.PSI == nil {
		return append(lines, "Memory PSI: unreported")
	}
	for _, row := range []struct {
		name  string
		value nodecontext.PSIData
	}{{"some", memory.PSI.Some}, {"full", memory.PSI.Full}} {
		lines = append(lines, fmt.Sprintf("PSI %s avg10/60/300: %.2f%% / %.2f%% / %.2f%%", row.name, row.value.Avg10, row.value.Avg60, row.value.Avg300),
			fmt.Sprintf("PSI %s cumulative stall: %d ns", row.name, row.value.TotalNanoseconds))
	}
	return lines
}

func swapLines(label string, swap *nodecontext.Swap, now time.Time) []string {
	if swap == nil {
		return []string{label + ": unreported"}
	}
	return []string{label + " sampled: " + sample(swap.CapturedAt, now), "Swap usage: " + bytes(swap.UsageBytes), "Swap available: " + bytes(swap.AvailableBytes)}
}

// Wrap removes terminal controls before wrapping; remote identities and offline
// files must not inject escape sequences. Width is in display cells.
func Wrap(lines []string, width int) []string {
	width = max(1, width)
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		clean := strings.Map(func(r rune) rune {
			if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
				return '\uFFFD'
			}
			return r
		}, line)
		result = append(result, strings.Split(ansi.Hardwrap(clean, width, true), "\n")...)
	}
	return result
}
