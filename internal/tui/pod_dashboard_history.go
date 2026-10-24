package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	memmodel "github.com/danushkastanley/kube-memlens/internal/model"
)

func (m appModel) podDashboardHistory(pod api.PodSnapshot, width int) []string {
	lines := []string{}
	if m.selectedHistory.loading {
		lines = append(lines, "Loading retained Pod history...")
	}
	if m.selectedHistory.err != nil {
		lines = append(lines, "History refresh failed; last good data.")
	}
	series := selectHistorySeries(pod, m.selectedHistory.series)
	if len(series.Points) == 0 {
		return append(lines, "No recent points retained for this Pod instance.")
	}
	lines = append(lines, podHistoryPlot(series.Points, pod.Memory, width)...)
	first, last := series.Points[0], series.Points[len(series.Points)-1]
	lines = append(lines,
		first.CapturedAt.UTC().Format("15:04:05")+" → "+last.CapturedAt.UTC().Format("15:04:05 UTC"),
		fmt.Sprintf("%d samples · retained %s", len(series.Points), last.CapturedAt.Sub(first.CapturedAt).Round(time.Second)),
		"Total: "+memmodel.FormatCompactBytes(last.TotalBytes)+" · Δ "+signedTrend(first.TotalBytes, last.TotalBytes),
		"Dots are observed samples; gaps are not interpolated.",
	)
	return lines
}

// Each glyph is an observed point in a time bin, not an interpolated rate or
// synthetic curve. The zero baseline avoids amplifying tiny stable fluctuations.
func podHistoryPlot(points []api.MemoryHistoryPoint, memory memmodel.MemoryBreakdown, width int) []string {
	if len(points) == 0 {
		return nil
	}
	const height = 4
	columns := maxInt(4, width-10)
	ceiling := uint64(1)
	for _, point := range points {
		ceiling = max(ceiling, point.TotalBytes)
	}
	finiteLimit := memory.MaxKnown && !memory.MaxUnlimited && memory.MaxBytes > 0
	if finiteLimit {
		ceiling = max(ceiling, memory.MaxBytes)
	}
	grid := make([][]rune, height)
	for row := range grid {
		grid[row] = []rune(strings.Repeat(" ", columns))
	}
	first, last := points[0].CapturedAt, points[len(points)-1].CapturedAt
	span := last.Sub(first).Seconds()
	for _, point := range points {
		column := 0
		if span > 0 {
			column = int(point.CapturedAt.Sub(first).Seconds() / span * float64(columns-1))
		}
		column = minInt(columns-1, maxInt(0, column))
		row := height - 1 - int(float64(point.TotalBytes)/float64(ceiling)*float64(height-1))
		grid[maxInt(0, row)][column] = '•'
	}
	lines := []string{"Scale: 0–" + memmodel.FormatCompactBytes(ceiling)}
	for _, row := range grid {
		lines = append(lines, "│"+string(row))
	}
	lines = append(lines, "└"+strings.Repeat("─", columns))
	if finiteLimit {
		lines = append(lines, "Current limit: "+memmodel.FormatCompactBytes(memory.MaxBytes)+" (reference)")
	}
	return lines
}
