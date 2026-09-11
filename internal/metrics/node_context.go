package metrics

import "github.com/danushkastanley/kube-memlens/internal/api"

func (r *renderer) renderNodeContext(debug *api.NodeContextDebug) {
	if debug == nil {
		return
	}
	r.helpType("kubememlens_node_context_store", "Bounded aggregate Node-context store counters.", "gauge")
	for _, item := range []struct {
		kind  string
		value int
	}{
		{"shared_node_records", debug.SharedNodeRecords}, {"records", debug.Records}, {"fresh_records", debug.FreshRecords}, {"stale_records", debug.StaleRecords},
		{"failed_records", debug.FailedRecords}, {"max_records", debug.MaxRecords}, {"history_series", debug.HistorySeries},
		{"history_points", debug.HistoryPoints}, {"history_bytes", debug.HistoryBytes}, {"max_history_series", debug.MaxHistorySeries},
		{"max_history_points", debug.MaxHistoryPoints}, {"max_history_bytes", debug.MaxHistoryBytes},
	} {
		r.sample("kubememlens_node_context_store", labels{"kind": item.kind}, float64(item.value))
	}
}
