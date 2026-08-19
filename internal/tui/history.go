package tui

import (
	"fmt"
	"strings"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/model"
)

func renderHistoryTrend(pod api.PodSnapshot, histories []api.PodHistory, width int) []string {
	series := selectHistorySeries(pod, histories)
	if len(series.Points) == 0 {
		return []string{"No recent points retained."}
	}
	maxPoints := width - 34
	if maxPoints < 8 {
		maxPoints = 8
	}
	points := series.Points
	if len(points) > maxPoints {
		points = points[len(points)-maxPoints:]
	}
	values := make([]uint64, len(points))
	var oom, kill, high, maxEvents uint64
	for i, point := range points {
		values[i] = point.TotalBytes
		oom += point.OOMEventsDelta
		kill += point.OOMKillEventsDelta
		high += point.HighEventsDelta
		maxEvents += point.MaxEventsDelta
	}
	first, last := points[0], points[len(points)-1]
	return []string{
		fmt.Sprintf("Total %s  %s → %s  Δ %s", sparkline(values), model.FormatCompactBytes(first.TotalBytes), model.FormatCompactBytes(last.TotalBytes), signedTrend(first.TotalBytes, last.TotalBytes)),
		fmt.Sprintf("Latest PSI some/full %.2f/%.2f%%  Events oom=%d kill=%d high=%d max=%d", last.PSISomeAvg10, last.PSIFullAvg10, oom, kill, high, maxEvents),
		"Latest reclaim " + tuiReclaim(last),
	}
}

func tuiReclaim(point api.MemoryHistoryPoint) string {
	if !point.ReclaimDeltasKnown {
		return "baseline not yet available"
	}
	efficiency := "n/a"
	if point.PageScanDelta > 0 {
		efficiency = fmt.Sprintf("%.0f%%", float64(point.PageStealDelta)/float64(point.PageScanDelta)*100)
	}
	return fmt.Sprintf("scan=%d steal=%d efficiency=%s refault=%d major=%d",
		point.PageScanDelta, point.PageStealDelta, efficiency,
		point.RefaultAnonDelta+point.RefaultFileDelta, point.MajorFaultsDelta)
}

func selectHistorySeries(pod api.PodSnapshot, histories []api.PodHistory) api.PodHistory {
	for _, history := range histories {
		if history.PodUID == pod.PodUID && history.NodeName == pod.NodeName {
			return history
		}
	}
	if pod.PodUID != "" {
		return api.PodHistory{}
	}
	for _, history := range histories {
		if history.NodeName == pod.NodeName {
			return history
		}
	}
	if len(histories) > 0 {
		return histories[0]
	}
	return api.PodHistory{}
}

func sparkline(values []uint64) string {
	if len(values) == 0 {
		return ""
	}
	const blocks = "▁▂▃▄▅▆▇█"
	min, max := values[0], values[0]
	for _, value := range values[1:] {
		if value < min {
			min = value
		}
		if value > max {
			max = value
		}
	}
	var b strings.Builder
	blockRunes := []rune(blocks)
	for _, value := range values {
		index := 0
		if max > min {
			index = int(float64(value-min) / float64(max-min) * float64(len(blockRunes)-1))
		}
		b.WriteRune(blockRunes[index])
	}
	return b.String()
}

func signedTrend(before, after uint64) string {
	if after >= before {
		return "+" + model.FormatCompactBytes(after-before)
	}
	return "-" + model.FormatCompactBytes(before-after)
}
