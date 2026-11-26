package explain

// Small numeric helpers shared by the diagnosis rules.

func countTrue(values ...bool) int {
	count := 0
	for _, value := range values {
		if value {
			count++
		}
	}
	return count
}

func componentRatio(part, total uint64) float64 {
	if total == 0 {
		return 0
	}
	return float64(part) / float64(total)
}

func minUint64(a, b uint64) uint64 {
	if a < b {
		return a
	}
	return b
}
