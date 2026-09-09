// Package qosview formats the shared MemoryQoS interpretation for terminal views.
package qosview

import (
	"fmt"
	"strconv"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/explain"
	"github.com/danushkastanley/kube-memlens/internal/model"
)

func Boundary(value explain.QoSBoundary) string {
	switch value.State {
	case explain.BoundaryUnavailable:
		return "unavailable"
	case explain.BoundaryUnlimited:
		return "unlimited"
	case explain.BoundaryZero, explain.BoundaryFinite:
		return model.FormatCompactBytes(value.Bytes)
	default:
		return "unknown"
	}
}

func ContainerLines(container api.ContainerSnapshot) []string {
	qos := explain.InterpretMemoryQoS(container)
	high := "unreported"
	if qos.DeltaKnown {
		high = strconv.FormatUint(qos.HighDelta, 10) + " (" + qos.EventSource + ")"
	}
	lines := []string{
		"MemoryQoS (container cgroup): " + string(qos.State) + "; confidence " + string(qos.Confidence),
		"Reclaim protection memory.min: " + Boundary(qos.Min),
		"Reclaim protection memory.low: " + Boundary(qos.Low),
		"Throttle boundary memory.high: " + Boundary(qos.High),
		"Hard limit memory.max: " + Boundary(qos.Max),
		"Throttle activity: " + string(qos.Activity) + "; recent high delta: " + high,
	}
	if qos.DeltaKnown {
		lines = append(lines, "High counter window: "+qos.DeltaStart.Format(time.RFC3339Nano)+" to "+qos.ObservedAt.Format(time.RFC3339Nano))
	}
	if qos.PSIKnown {
		lines = append(lines, fmt.Sprintf("Memory PSI avg10: some %.2f%% / full %.2f%%", qos.PSISomeAvg10, qos.PSIFullAvg10))
	}
	if qos.PodConfiguredLimit.Known {
		lines = append(lines, "Pod configured limit: "+model.FormatCompactBytes(qos.PodConfiguredLimit.Bytes)+"; parent cgroup controls are not observed")
	}
	for _, line := range qos.Correlations {
		lines = append(lines, "Resource correlation: "+line)
	}
	for _, line := range qos.Caveats {
		lines = append(lines, "Caveat: "+line)
	}
	for _, line := range qos.SuggestedChecks {
		lines = append(lines, "Check: "+line)
	}
	return lines
}

func PodLines(pod api.PodSnapshot) []string {
	if len(pod.Containers) == 0 {
		return []string{"MemoryQoS: complete container evidence is unavailable"}
	}
	var lines []string
	for _, container := range pod.Containers {
		lines = append(lines, "Container "+container.ContainerName+":")
		lines = append(lines, ContainerLines(container)...)
	}
	return lines
}
