package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/rivo/uniseg"
)

func FormatAge(capturedAt time.Time) string {
	if capturedAt.IsZero() {
		return "-"
	}
	age := time.Since(capturedAt)
	if age < 0 {
		age = 0
	}
	age = age.Round(time.Second)
	if age < time.Minute {
		return fmt.Sprintf("%ds", int(age.Seconds()))
	}
	if age < time.Hour {
		return fmt.Sprintf("%dm", int(age.Minutes()))
	}
	if age < 24*time.Hour {
		return fmt.Sprintf("%dh", int(age.Hours()))
	}
	return fmt.Sprintf("%dd", int(age.Hours()/24))
}

func formatPodAge(pod api.PodSnapshot) string {
	return FormatAge(pod.Context.CreatedAt)
}

func formatPodCreatedAt(pod api.PodSnapshot) string {
	if pod.Context.CreatedAt.IsZero() {
		return "unknown"
	}
	return pod.Context.CreatedAt.UTC().Format("2006-01-02 15:04:05 MST") + " (" + formatPodAge(pod) + " ago)"
}

func truncate(value string, width int) string {
	if width <= 0 {
		return ""
	}
	if uniseg.StringWidth(value) <= width {
		return value
	}
	if width == 1 {
		return "…"
	}
	return trimToWidth(value, width-1) + "…"
}

func trimToWidth(value string, width int) string {
	var b strings.Builder
	used := 0
	graphemes := uniseg.NewGraphemes(value)
	for graphemes.Next() {
		cluster := graphemes.Str()
		clusterWidth := uniseg.StringWidth(cluster)
		if used+clusterWidth > width {
			break
		}
		b.WriteString(cluster)
		used += clusterWidth
	}
	return b.String()
}

func pad(value string, width int, alignRight bool) string {
	value = truncate(value, width)
	spaces := width - uniseg.StringWidth(value)
	if spaces <= 0 {
		return value
	}
	if alignRight {
		return strings.Repeat(" ", spaces) + value
	}
	return value + strings.Repeat(" ", spaces)
}
