package cli

import (
	"fmt"

	"github.com/danushkastanley/kube-memlens/internal/api"
)

func (r *doctorReport) addNodeContextChecks(value *api.NodeContextDebug) {
	if value == nil {
		return
	}
	summary := fmt.Sprintf("%d Node-context records; %d/%d shared Node slots; %d fresh, %d stale, %d source failures; history %d/%d series and %d/%d bytes",
		value.Records, value.SharedNodeRecords, value.MaxRecords, value.FreshRecords, value.StaleRecords, value.FailedRecords,
		value.HistorySeries, value.MaxHistorySeries, value.HistoryBytes, value.MaxHistoryBytes)
	status := "pass"
	switch {
	case value.MaxRecords <= 0 || value.MaxHistoryBytes <= 0 || value.MaxHistorySeries <= 0 || value.MaxHistoryPoints <= 0:
		status = "fail"
	case value.Records == 0 || value.StaleRecords > 0 || value.FailedRecords > 0 || value.CoverageLost ||
		value.SharedNodeRecords >= value.MaxRecords || value.HistorySeries >= value.MaxHistorySeries || value.HistoryBytes >= value.MaxHistoryBytes:
		status = "warn"
	}
	r.addCheck("Node context", status, summary)
}
