package tui

import (
	"strings"

	"github.com/rivo/uniseg"
)

// wrapText keeps source caveats readable in narrow detail panels. Grapheme
// boundaries are preserved, including while the terminal is being resized.
func wrapText(lines []string, width int) []string {
	var result []string
	for _, line := range lines {
		for uniseg.StringWidth(line) > width {
			prefix := trimToWidth(line, width)
			if prefix == "" {
				result = append(result, truncate(line, width))
				line = ""
				break
			}
			if space := strings.LastIndex(prefix, " "); space > 0 {
				prefix = prefix[:space]
			}
			result = append(result, prefix)
			line = strings.TrimLeft(line[len(prefix):], " ")
		}
		result = append(result, line)
	}
	return result
}
