package nodeview

import (
	"fmt"
	"sort"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
)

func HistoryLines(history api.NodeContextHistory, width int) []string {
	lines := []string{"Node history: " + history.NodeName, fmt.Sprintf("Window: %ds; completeness: %s; coverage lost: %t", history.WindowSeconds, history.Completeness, history.CoverageLost), "Collector reset: " + instant(history.ResetAt)}
	for index, series := range history.Series {
		lines = append(lines, fmt.Sprintf("Instance %d (%d points):", index+1, len(series.Points)))
		points := append([]api.NodeContextHistoryPoint(nil), series.Points...)
		sort.SliceStable(points, func(i, j int) bool { return points[i].ReceivedAt.Before(points[j].ReceivedAt) })
		for _, point := range points {
			o := point.Observation
			lines = append(lines, "Received: "+instant(point.ReceivedAt)+"; source: "+instant(o.Evidence.CapturedAt)+"; completeness: "+string(o.Evidence.Completeness))
			if o.Stats == nil || o.Stats.Memory == nil {
				lines = append(lines, "Memory: unreported")
				continue
			}
			m := o.Stats.Memory
			lines = append(lines, "Node boot: "+instant(o.Stats.StartedAt))
			lines = append(lines, "Usage "+bytes(m.UsageBytes)+"; available "+bytes(m.AvailableBytes)+"; working set "+bytes(m.WorkingSetBytes)+"; RSS "+bytes(m.RSSBytes))
			if m.PSI != nil {
				lines = append(lines, fmt.Sprintf("PSI some/full avg10 %.2f%% / %.2f%%", m.PSI.Some.Avg10, m.PSI.Full.Avg10))
			}
			if o.Stats.Swap != nil {
				lines = append(lines, "Swap usage "+bytes(o.Stats.Swap.UsageBytes)+"; sampled "+instant(o.Stats.Swap.CapturedAt))
			}
		}
	}
	if len(history.Series) == 0 {
		lines = append(lines, "No retained points; history may be rebuilding.")
	}
	return Wrap(lines, width)
}

func HistorySince(history api.NodeContextHistory, cutoff time.Time) api.NodeContextHistory {
	series := make([]api.NodeContextHistorySeries, 0, len(history.Series))
	for _, item := range history.Series {
		points := make([]api.NodeContextHistoryPoint, 0, len(item.Points))
		for _, point := range item.Points {
			if !point.Observation.Evidence.CapturedAt.Before(cutoff) {
				points = append(points, point)
			}
		}
		if len(points) > 0 {
			item.Points = points
			series = append(series, item)
		}
	}
	history.Series = series
	return history
}
