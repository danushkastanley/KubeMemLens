package agent

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

const MetricsContentType = "application/openmetrics-text; version=1.0.0; charset=utf-8"

type Telemetry struct {
	mu sync.RWMutex

	scanSuccess uint64
	scanFailure uint64
	postSuccess uint64
	postFailure uint64

	lastScanAt        time.Time
	lastScanDuration  time.Duration
	containersFound   int
	mapped            int
	unmapped          int
	infrastructure    int
	metadataCachePods int
}

func (t *Telemetry) RecordScan(at time.Time, duration time.Duration, result ScanResult, err error, metadataCachePods int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if err != nil {
		t.scanFailure++
	} else {
		t.scanSuccess++
	}
	t.lastScanAt = at
	t.lastScanDuration = duration
	t.containersFound = result.ContainersFound
	t.mapped = result.Mapped
	t.unmapped = result.Unmapped
	t.infrastructure = result.InfrastructureCgroups
	t.metadataCachePods = metadataCachePods
}

func (t *Telemetry) RecordPost(err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if err != nil {
		t.postFailure++
	} else {
		t.postSuccess++
	}
}

func (t *Telemetry) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", MetricsContentType)
		_, _ = w.Write([]byte(t.Render()))
	})
	return mux
}

func (t *Telemetry) Render() string {
	t.mu.RLock()
	defer t.mu.RUnlock()

	var b strings.Builder
	writeMetricHeader(&b, "kubememlens_agent_scans_total", "KubeMemLens agent scan attempts by result.", "counter")
	fmt.Fprintf(&b, "kubememlens_agent_scans_total{result=\"success\"} %d\n", t.scanSuccess)
	fmt.Fprintf(&b, "kubememlens_agent_scans_total{result=\"failure\"} %d\n", t.scanFailure)
	writeMetricHeader(&b, "kubememlens_agent_snapshot_posts_total", "KubeMemLens agent snapshot post attempts by result.", "counter")
	fmt.Fprintf(&b, "kubememlens_agent_snapshot_posts_total{result=\"success\"} %d\n", t.postSuccess)
	fmt.Fprintf(&b, "kubememlens_agent_snapshot_posts_total{result=\"failure\"} %d\n", t.postFailure)
	writeMetricHeader(&b, "kubememlens_agent_last_scan_timestamp_seconds", "Unix timestamp of the latest KubeMemLens agent scan attempt.", "gauge")
	fmt.Fprintf(&b, "kubememlens_agent_last_scan_timestamp_seconds %d\n", unixOrZero(t.lastScanAt))
	writeMetricHeader(&b, "kubememlens_agent_last_scan_duration_seconds", "Duration in seconds of the latest KubeMemLens agent scan attempt.", "gauge")
	fmt.Fprintf(&b, "kubememlens_agent_last_scan_duration_seconds %g\n", t.lastScanDuration.Seconds())
	writeMetricHeader(&b, "kubememlens_agent_last_scan_containers", "Container cgroup counts from the latest KubeMemLens agent scan.", "gauge")
	fmt.Fprintf(&b, "kubememlens_agent_last_scan_containers{kind=\"found\"} %d\n", t.containersFound)
	fmt.Fprintf(&b, "kubememlens_agent_last_scan_containers{kind=\"mapped\"} %d\n", t.mapped)
	fmt.Fprintf(&b, "kubememlens_agent_last_scan_containers{kind=\"unmapped\"} %d\n", t.unmapped)
	fmt.Fprintf(&b, "kubememlens_agent_last_scan_containers{kind=\"infrastructure\"} %d\n", t.infrastructure)
	writeMetricHeader(&b, "kubememlens_agent_metadata_cache_pods", "Pods in the node-filtered KubeMemLens agent metadata cache.", "gauge")
	fmt.Fprintf(&b, "kubememlens_agent_metadata_cache_pods %d\n", t.metadataCachePods)
	b.WriteString("# EOF\n")
	return b.String()
}

func writeMetricHeader(b *strings.Builder, name, help, metricType string) {
	fmt.Fprintf(b, "# HELP %s %s\n", name, help)
	fmt.Fprintf(b, "# TYPE %s %s\n", name, metricType)
}

func unixOrZero(value time.Time) int64 {
	if value.IsZero() {
		return 0
	}
	return value.Unix()
}
