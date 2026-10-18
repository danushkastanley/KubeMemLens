// Package historyview renders bounded, source-labelled trends for the CLI and TUI.
package historyview

import (
	"fmt"
	"strings"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/memoryhistory"
	"github.com/danushkastanley/kube-memlens/internal/model"
)

func Lines(r memoryhistory.Report, now time.Time, width int) []string {
	current := r
	current.Series = append([]memoryhistory.Series(nil), r.Series...)
	if r.State != memoryhistory.Unsupported && r.State != memoryhistory.Disabled {
		current.ReceivedAt = now
		memoryhistory.Summarise(&current, r.Completeness == memoryhistory.Partial)
	}
	lines := []string{
		fmt.Sprintf("Memory history · %s · %s", r.Query.Source, r.Query.Metric),
		fmt.Sprintf("%s to %s UTC · step %s", r.Query.Start.UTC().Format("02 Jan 15:04"), r.Query.End.UTC().Format("02 Jan 15:04"), r.Query.Step),
		fmt.Sprintf("Freshness: %s · coverage: %s", current.State, r.Completeness),
	}
	if r.Query.Source == memoryhistory.Prometheus && r.Selection.Request.Scope != memoryhistory.Node {
		lines = append(lines, "Current running container instances; earlier instances excluded.")
	}
	if r.Selection.Request.Scope == memoryhistory.Workload {
		lines = append(lines, "Current workload members only; earlier replicas are excluded.")
	}
	if r.State == memoryhistory.Disabled {
		return append(lines, "Local Node collection is disabled; Prometheus remains a separate source.")
	}
	if r.State == memoryhistory.Unsupported {
		return append(lines, "This source does not retain the selected metric and scope.")
	}
	lines = append(lines, "Working set, RSS and cgroup charge are different measurements.", "Plots: . gap; ! stale. Each series uses its own scale.")
	lines = append(lines, "Fetched "+r.ReceivedAt.UTC().Format("02 Jan 15:04:05")+" UTC; refresh manually.")
	for _, s := range current.Series {
		name := s.Target.Node
		if s.Target.Pod != "" {
			name = s.Target.Namespace + "/" + s.Target.Pod
		}
		if s.Target.Container != "" {
			name += "/" + s.Target.Container
		}
		missing, stale := 0, 0
		latest := "unreported"
		sampled := "unreported"
		for _, p := range s.Points {
			if p.State == memoryhistory.Missing {
				missing++
			}
			if p.State == memoryhistory.Stale {
				stale++
			}
			if p.Bytes != nil {
				latest = model.FormatCompactBytes(*p.Bytes)
				sampled = p.SampledAt.UTC().Format("02 Jan 15:04:05") + " UTC"
			}
		}
		lines = append(lines, "", name+" · "+s.Origin+" · "+string(s.Freshness),
			Sparkline(s.Points, max(8, min(72, width-4))),
			fmt.Sprintf("Latest retained: %s · gaps %d · stale points %d", latest, missing, stale),
			"Sample: "+sampled+" · "+s.SampleClock)
	}
	return lines
}

// Each bucket keeps the latest value, but any gap or stale sample in the bucket
// remains visible. Plot compression cannot turn incomplete history into a line.
func Sparkline(points []memoryhistory.Point, columns int) string {
	columns = max(1, min(columns, len(points)))
	if len(points) == 0 {
		return "No retained samples."
	}
	var low, high uint64
	found := false
	for _, p := range points {
		if p.Bytes == nil {
			continue
		}
		if !found {
			low, high, found = *p.Bytes, *p.Bytes, true
		}
		low = min(low, *p.Bytes)
		high = max(high, *p.Bytes)
	}
	blocks := []rune("▁▂▃▄▅▆▇█")
	var out strings.Builder
	for column := 0; column < columns; column++ {
		start, end := column*len(points)/columns, (column+1)*len(points)/columns
		marker := rune(0)
		var value uint64
		for _, p := range points[start:end] {
			if p.Bytes == nil {
				marker = '.'
				continue
			}
			value = *p.Bytes
			if p.State == memoryhistory.Stale && marker != '.' {
				marker = '!'
			}
		}
		if marker != 0 {
			out.WriteRune(marker)
			continue
		}
		index := 0
		if high > low {
			index = int(float64(value-low) / float64(high-low) * 7)
		}
		out.WriteRune(blocks[index])
	}
	return out.String()
}
