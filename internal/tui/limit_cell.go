package tui

import (
	"fmt"

	"github.com/rivo/uniseg"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/explain"
)

// minClosestNameWidth is the shortest container name worth keeping the bar
// for. A name squeezed below it is harder to read than a cell without a bar.
const minClosestNameWidth = 6

// podLimitCell is the Pod's limit label, followed by the container closest to
// its own limit when the Pod figure hides it, for example
// "████░░░░ 18% (app 99%)". To fit width it shortens the container name first
// and then drops the bar, so the container's percentage is the last to go.
func podLimitCell(pod api.PodSnapshot, width int, ascii bool) string {
	label := limitLabel(pod.Memory, 8, ascii)
	usage, ok := explain.ClosestContainerToLimit(pod)
	if !ok {
		return label
	}
	if cell, fits := closestContainerCell(label, usage, width, minClosestNameWidth); fits {
		return cell
	}
	cell, _ := closestContainerCell(limitLabel(pod.Memory, 0, ascii), usage, width, 1)
	return cell
}

// closestContainerCell appends the container to label, shortening its name
// to fit width. It reports false when the name would fall below minName.
func closestContainerCell(label string, usage explain.ContainerLimitUsage, width, minName int) (string, bool) {
	prefix, suffix := label+" (", fmt.Sprintf(" %d%%)", usage.Percent)
	room := width - uniseg.StringWidth(prefix) - uniseg.StringWidth(suffix)
	name := usage.ContainerName
	if room < minInt(minName, uniseg.StringWidth(name)) {
		return prefix + truncate(name, minName) + suffix, false
	}
	return prefix + truncate(name, room) + suffix, true
}

// closestContainerLine is the detail-pane sentence for the same fact, or
// false when the Pod figure already tells the whole story.
func closestContainerLine(pod api.PodSnapshot) (string, bool) {
	usage, ok := explain.ClosestContainerToLimit(pod)
	if !ok {
		return "", false
	}
	return fmt.Sprintf("%s at %d%% of its limit", usage.ContainerName, usage.Percent), true
}
