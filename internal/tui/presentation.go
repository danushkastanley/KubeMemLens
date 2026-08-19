package tui

import (
	"fmt"
	"math/bits"
	"strings"

	"github.com/danushkastanley/kube-memlens/internal/explain"
	"github.com/danushkastanley/kube-memlens/internal/model"
)

type memoryPresentation struct {
	composition string
	limit       string
	signal      string
	severity    string
	diagnosis   string
}

func presentMemory(memory model.MemoryBreakdown, barWidth int, ascii bool) memoryPresentation {
	result := explain.Analyze(memory)
	return memoryPresentation{
		composition: compositionBar(memory, barWidth, ascii),
		limit:       limitLabel(memory, 8, ascii),
		signal:      incidentLabel(memory),
		severity:    severityLabel(result.Severity),
		diagnosis:   string(result.Diagnosis),
	}
}

func compositionBar(memory model.MemoryBreakdown, width int, ascii bool) string {
	if width < 4 {
		width = 4
	}
	if memory.TotalBytes == 0 {
		return strings.Repeat("·", width)
	}
	values := boundedComposition(memory)
	characters := []rune{'█', '▓', '▒', '░'}
	if ascii {
		characters = []rune{'A', 'F', 'S', 'O'}
	}
	counts := make([]int, len(values))
	used := 0
	for index := 0; index < len(values)-1; index++ {
		counts[index] = scaledWidth(values[index], width, memory.TotalBytes)
		used += counts[index]
	}
	counts[len(counts)-1] = width - used
	var output strings.Builder
	for index, count := range counts {
		output.WriteString(strings.Repeat(string(characters[index]), count))
	}
	return output.String()
}

func scaledWidth(value uint64, width int, total uint64) int {
	if value == 0 || width <= 0 || total == 0 {
		return 0
	}
	high, low := bits.Mul64(value, uint64(width))
	quotient, _ := bits.Div64(high, low, total)
	if quotient > uint64(width) {
		return width
	}
	return int(quotient)
}

func boundedComposition(memory model.MemoryBreakdown) []uint64 {
	remaining := memory.TotalBytes
	values := []uint64{memory.RSSBytes(), memory.CacheBytes(), memory.ShmemBytes, memory.ResidualBytes()}
	for index, value := range values {
		if value > remaining {
			value = remaining
		}
		values[index] = value
		remaining -= value
	}
	values[len(values)-1] += remaining
	return values
}

func limitLabel(memory model.MemoryBreakdown, width int, ascii bool) string {
	if !memory.MaxKnown {
		return "unknown"
	}
	if memory.MaxUnlimited || memory.MaxBytes == 0 {
		return "unlimited"
	}
	ratio := memory.LimitUsageRatio()
	percent := ratio * 100
	if width < 4 {
		return fmt.Sprintf("%.0f%%", percent)
	}
	filled := int(ratio * float64(width))
	if filled < 0 {
		filled = 0
	}
	if filled > width {
		filled = width
	}
	full, empty := "█", "░"
	if ascii {
		full, empty = "#", "-"
	}
	return fmt.Sprintf("%s%s %.0f%%", strings.Repeat(full, filled), strings.Repeat(empty, width-filled), percent)
}

func incidentLabel(memory model.MemoryBreakdown) string {
	oom, kill, high, maxEvents := memory.RecentEventCounts()
	parts := make([]string, 0, 3)
	if kill > 0 || oom > 0 {
		parts = append(parts, fmt.Sprintf("OOM %d/%d", oom, kill))
	}
	if maxEvents > 0 {
		parts = append(parts, fmt.Sprintf("max %d", maxEvents))
	}
	if high > 0 {
		parts = append(parts, fmt.Sprintf("high %d", high))
	}
	if memory.PressureKnown && (memory.PSISomeAvg10 >= 1 || memory.PSIFullAvg10 > 0) {
		parts = append(parts, fmt.Sprintf("PSI %.1f/%.1f", memory.PSISomeAvg10, memory.PSIFullAvg10))
	}
	if len(parts) == 0 {
		if !memory.EventDeltasKnown && !memory.LocalEventDeltasKnown {
			return "baseline"
		}
		return "clear"
	}
	return strings.Join(parts, " ")
}

func severityLabel(severity explain.Severity) string {
	switch severity {
	case explain.SeverityCritical:
		return "CRIT"
	case explain.SeverityHigh:
		return "HIGH"
	case explain.SeverityMedium:
		return "MED"
	default:
		return "INFO"
	}
}

func trendLabel(direction int8) string {
	switch {
	case direction > 0:
		return "↑"
	case direction < 0:
		return "↓"
	default:
		return "→"
	}
}
