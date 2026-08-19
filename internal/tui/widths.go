package tui

func namespaceWidths(total int) []int {
	fixed := []int{5, 8, 8, 8, 7, 8, 18, 6}
	return withDynamic(total, fixed, 14)
}

func podWidths(total int) []int {
	fixed := []int{12, 10, 8, 8, 8, 7, 8, 18, 6}
	return withDynamic(total, fixed, 20)
}

func workloadWidths(total int) []int {
	fixed := []int{12, 6, 8, 8, 8, 7, 8, 16, 8, 18}
	return withTwoDynamic(total, fixed, 14, 18)
}

func containerWidths(total int) []int {
	fixed := []int{12, 10, 8, 8, 8, 7, 8, 18, 6}
	return withTwoDynamic(total, fixed, 18, 16)
}

func withDynamic(total int, fixed []int, minDynamic int) []int {
	gaps := 2 * len(fixed)
	used := gaps
	for _, width := range fixed {
		used += width
	}
	dynamic := total - used - 2
	if dynamic < minDynamic {
		dynamic = minDynamic
	}
	widths := []int{dynamic}
	widths = append(widths, fixed...)
	return widths
}

func withTwoDynamic(total int, fixed []int, minFirst int, minSecond int) []int {
	gaps := 2 * (len(fixed) + 1)
	used := gaps
	for _, width := range fixed {
		used += width
	}
	remaining := total - used - 2
	first := remaining / 2
	second := remaining - first
	if first < minFirst {
		first = minFirst
	}
	if second < minSecond {
		second = minSecond
	}
	widths := []int{fixed[0], first, second}
	widths = append(widths, fixed[1:]...)
	return widths
}
