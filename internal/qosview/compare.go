package qosview

import (
	"fmt"
	"sort"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/explain"
)

func ComparisonLines(before, after api.PodSnapshot) []string {
	left, right := map[string]api.ContainerSnapshot{}, map[string]api.ContainerSnapshot{}
	names := map[string]bool{}
	for _, c := range before.Containers {
		left[c.ContainerName] = c
		names[c.ContainerName] = true
	}
	for _, c := range after.Containers {
		right[c.ContainerName] = c
		names[c.ContainerName] = true
	}
	order := make([]string, 0, len(names))
	for name := range names {
		order = append(order, name)
	}
	sort.Strings(order)
	var lines []string
	for _, name := range order {
		a, b := explain.InterpretMemoryQoS(left[name]), explain.InterpretMemoryQoS(right[name])
		for _, value := range []struct {
			name          string
			before, after explain.QoSBoundary
		}{
			{"reclaim protection memory.min", a.Min, b.Min}, {"reclaim protection memory.low", a.Low, b.Low},
			{"throttle boundary memory.high", a.High, b.High}, {"hard limit memory.max", a.Max, b.Max},
		} {
			if value.before != value.after {
				lines = append(lines, "Container "+name+" "+value.name+": "+Boundary(value.before)+" -> "+Boundary(value.after))
			}
		}
		if a.HighDelta != b.HighDelta || a.DeltaKnown != b.DeltaKnown || a.EventSource != b.EventSource {
			lines = append(lines, "Container "+name+" high-event delta: "+highDeltaText(a)+" -> "+highDeltaText(b))
		}
		if a.Activity != b.Activity {
			lines = append(lines, "Container "+name+" throttle activity: "+string(a.Activity)+" -> "+string(b.Activity))
		}
		if a.State != b.State {
			lines = append(lines, "Container "+name+" MemoryQoS evidence: "+string(a.State)+" -> "+string(b.State))
		}
	}
	return lines
}

func highDeltaText(qos explain.MemoryQoS) string {
	if !qos.DeltaKnown {
		return "unreported"
	}
	return fmt.Sprintf("%d (%s; %s to %s)", qos.HighDelta, qos.EventSource, qos.DeltaStart.Format(time.RFC3339Nano), qos.ObservedAt.Format(time.RFC3339Nano))
}
