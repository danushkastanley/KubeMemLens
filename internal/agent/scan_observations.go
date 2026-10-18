package agent

import (
	"encoding/json"
	"net/http"
	"time"
)

// A fixed history lets diagnostics recover back-to-back completions without
// retaining workload identifiers or turning a polling gap into invented data.
const scanHistoryLimit = 32

type scanTiming struct {
	Sequence           uint64 `json:"sequence"`
	CompletedUnixNanos int64  `json:"completedUnixNanos"`
	DurationNanos      int64  `json:"durationNanos"`
	Result             string `json:"result"`
}

type scanObservations struct {
	SchemaVersion int          `json:"schemaVersion"`
	Metrics       string       `json:"metrics"`
	RecentScans   []scanTiming `json:"recentScans"`
}

// RecordScan owns the lock and has already advanced the outcome counters.
func (t *Telemetry) recordTiming(at time.Time, duration time.Duration, err error) {
	sequence := t.scanSuccess + t.scanFailure
	result := "success"
	if err != nil {
		result = "failure"
	}
	t.recentScans[(sequence-1)%scanHistoryLimit] = scanTiming{sequence, at.UnixNano(), int64(duration), result}
}

func (t *Telemetry) scanObservations() scanObservations {
	t.mu.RLock()
	defer t.mu.RUnlock()
	total := t.scanSuccess + t.scanFailure
	count := min(total, scanHistoryLimit)
	recent := make([]scanTiming, 0, count)
	for offset := range count {
		sequence := total - count + offset + 1
		recent = append(recent, t.recentScans[(sequence-1)%scanHistoryLimit])
	}
	return scanObservations{1, t.renderLocked(), recent}
}

func (t *Telemetry) serveScanObservations(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(t.scanObservations())
}
