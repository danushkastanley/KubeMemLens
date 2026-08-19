package tui

import (
	"fmt"

	memmodel "github.com/danushkastanley/kube-memlens/internal/model"
)

func (m appModel) renderNodes(width int) string {
	items := m.visibleNodes()
	if len(items) == 0 {
		return "No nodes match the current filter."
	}
	compact := width < 105
	var widths []int
	var lines []string
	if compact {
		widths = withDynamic(width, []int{5, 6, 9, 10, 6}, 18)
		lines = []string{tableRow([]string{"NODE", "PODS", "CONTS", "CHARGE", "PRESSURE", "AGE"}, widths, nil)}
	} else {
		widths = withDynamic(width, []int{5, 6, 10, 12, 16, 9, 7}, 22)
		lines = []string{tableRow([]string{"NODE", "PODS", "CONTS", "POD CHARGE", "ALLOCATABLE", "RUNTIME", "PRESSURE", "AGE"}, widths, nil)}
	}
	viewport := m.viewports[viewNodes]
	viewport.reconcile(len(items))
	start, end := viewport.visibleRange()
	for index := start; index < end; index++ {
		item := items[index]
		prefix := " "
		if index == viewport.selected {
			prefix = "›"
		}
		if compact {
			lines = append(lines, prefix+tableRow([]string{
				item.name,
				fmt.Sprintf("%d", item.podCount),
				fmt.Sprintf("%d", item.containerCount),
				memmodel.FormatCompactBytes(item.memory.TotalBytes),
				nodePressureLabel(item),
				FormatAge(item.capturedAt),
			}, widths, numericIndexes(1, 2, 3)))
			continue
		}
		allocatable := "unknown"
		if item.environment.MemoryAllocatableKnown {
			allocatable = memmodel.FormatCompactBytes(item.environment.MemoryAllocatableBytes)
		}
		lines = append(lines, prefix+tableRow([]string{
			item.name,
			fmt.Sprintf("%d", item.podCount),
			fmt.Sprintf("%d", item.containerCount),
			memmodel.FormatCompactBytes(item.memory.TotalBytes),
			allocatable,
			joinValues(item.environment.ContainerRuntimes),
			nodePressureLabel(item),
			FormatAge(item.capturedAt),
		}, widths, numericIndexes(1, 2, 3, 4)))
	}
	return truncateLines(lines, width)
}

func joinValues(values []string) string {
	if len(values) == 0 {
		return "unknown"
	}
	result := values[0]
	for _, value := range values[1:] {
		result += "," + value
	}
	return result
}
