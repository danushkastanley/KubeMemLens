package agent

import (
	"net/http"
	"time"
)

// LivenessHandler serves /livez and /healthz and nothing else. It is meant for
// a listener the kubelet can reach, so it exposes no metrics and no workload
// names. /livez fails when no scan has been attempted within window, measured
// from the latest attempt or, before the first one, from started. A failed
// scan still counts as an attempt: the loop is running.
func (t *Telemetry) LivenessHandler(started time.Time, window time.Duration, now func() time.Time) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("GET /livez", func(w http.ResponseWriter, _ *http.Request) {
		if now().Sub(t.lastScanAttempt(started)) > window {
			http.Error(w, "no scan attempt within the liveness window", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("ok\n"))
	})
	return mux
}

func (t *Telemetry) lastScanAttempt(started time.Time) time.Time {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if t.lastScanAt.After(started) {
		return t.lastScanAt
	}
	return started
}
