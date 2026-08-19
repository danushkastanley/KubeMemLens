package agent

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestTelemetryRecordsScanAndPostOutcomes(t *testing.T) {
	telemetry := &Telemetry{}
	telemetry.RecordScan(time.Unix(100, 0).UTC(), 250*time.Millisecond, ScanResult{
		ContainersFound:       4,
		Mapped:                3,
		Unmapped:              1,
		InfrastructureCgroups: 2,
	}, nil, 2)
	telemetry.RecordScan(time.Unix(101, 0).UTC(), 500*time.Millisecond, ScanResult{}, errors.New("scan failed"), 2)
	telemetry.RecordPost(nil)
	telemetry.RecordPost(errors.New("post failed"))

	metrics := telemetry.Render()
	for _, want := range []string{
		`kubememlens_agent_scans_total{result="success"} 1`,
		`kubememlens_agent_scans_total{result="failure"} 1`,
		`kubememlens_agent_snapshot_posts_total{result="success"} 1`,
		`kubememlens_agent_snapshot_posts_total{result="failure"} 1`,
		`kubememlens_agent_last_scan_duration_seconds 0.5`,
		`kubememlens_agent_metadata_cache_pods 2`,
		`kubememlens_agent_last_scan_containers{kind="infrastructure"} 0`,
		"# EOF",
	} {
		if !strings.Contains(metrics, want) {
			t.Fatalf("metrics missing %q:\n%s", want, metrics)
		}
	}
}

func TestTelemetryHandler(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	(&Telemetry{}).Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != MetricsContentType {
		t.Fatalf("Content-Type = %q", got)
	}
}
