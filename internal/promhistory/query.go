package promhistory

import (
	"context"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/memoryhistory"
)

const fieldLabel = "kml_history_field"

func (c *Client) Query(ctx context.Context, s memoryhistory.Selection, q memoryhistory.Query) (report memoryhistory.Report, err error) {
	r, err := memoryhistory.NewReport(s, q, c.now())
	if err != nil {
		return memoryhistory.Report{}, err
	}
	if q.Source != memoryhistory.Prometheus || q.Metric == memoryhistory.Charge {
		r.State, r.Reason = memoryhistory.Unsupported, "metric-not-supported"
		r.Series = nil
		return r, nil
	}
	for i := range r.Series {
		r.Series[i].Origin = "cadvisor"
		r.Series[i].SampleClock = "prometheus-sample"
	}
	select {
	case c.gate <- struct{}{}:
		defer func() { <-c.gate }()
	default:
		return memoryhistory.Report{}, memoryhistory.ErrBounds
	}
	ctx, cancel := context.WithTimeout(ctx, memoryhistory.QueryTimeout)
	defer cancel()
	// Cancellation wins over a response/decoder error observed at the same
	// boundary. Run this before our own cancel so successful queries stay valid.
	defer func() {
		if cause := ctx.Err(); cause != nil {
			report, err = memoryhistory.Report{}, cause
		}
	}()
	selectors := make([]string, len(s.Targets))
	for i, t := range s.Targets {
		selectors[i] = selector(c.cluster, t, q.Metric)
	}
	expression := "(" + strings.Join(selectors, " or ") + ")"
	// A single evaluation returns both values and their original Prometheus
	// sample timestamps. Separate requests could observe different scrapes.
	expression = `label_replace(` + expression + `, "` + fieldLabel + `", "value", "__name__", ".*") or label_replace(timestamp(` + expression + `), "` + fieldLabel + `", "sampled", "__name__", ".*")`
	values := url.Values{"query": {expression}, "start": {strconv.FormatInt(q.Start.Unix(), 10)}, "end": {strconv.FormatInt(q.End.Unix(), 10)}, "step": {strconv.FormatInt(int64(q.Step/time.Second), 10)}, "timeout": {"4s"}, "limit": {strconv.Itoa(2*memoryhistory.MaxTargets + 1)}, "lookback_delta": {"5m"}}
	data, err := c.fetch(ctx, values)
	if err != nil {
		return memoryhistory.Report{}, err
	}
	r.ReceivedAt = c.now()
	if err := decode(ctx, data, c.cluster, &r); err != nil {
		return memoryhistory.Report{}, err
	}
	return r, nil
}

func metricName(m memoryhistory.Metric) string {
	if m == memoryhistory.RSS {
		return "container_memory_rss"
	}
	return "container_memory_working_set_bytes"
}

func targetLabels(cluster string, t memoryhistory.Target) map[string]string {
	labels := map[string]string{"cluster": cluster, "node": t.Node, "node_uid": t.NodeUID}
	if t.PodUID == "" {
		labels["id"] = "/"
		labels["namespace"], labels["pod"], labels["container"] = "", "", ""
		return labels
	}
	labels["namespace"], labels["pod"], labels["pod_uid"] = t.Namespace, t.Pod, t.PodUID
	labels["container"], labels["container_id"] = t.Container, t.ContainerID
	return labels
}

func selector(cluster string, t memoryhistory.Target, m memoryhistory.Metric) string {
	labels := targetLabels(cluster, t)
	keys := make([]string, 0, len(labels))
	for key := range labels {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+strconv.Quote(labels[key]))
	}
	return metricName(m) + "{" + strings.Join(parts, ",") + "}"
}
