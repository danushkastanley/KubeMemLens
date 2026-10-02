package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func observedScans(t *testing.T, telemetry *Telemetry) scanObservations {
	t.Helper()
	response := httptest.NewRecorder()
	telemetry.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/scan-observations", nil))
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "application/json" {
		t.Fatal("scan diagnostics response failed")
	}
	var value scanObservations
	if err := json.Unmarshal(response.Body.Bytes(), &value); err != nil {
		t.Fatal(err)
	}
	return value
}

func TestScanObservationsRetainBackToBackCompletions(t *testing.T) {
	telemetry := &Telemetry{}
	at := time.Unix(1700000000, 123456789)
	telemetry.RecordScan(at, 5*time.Second, ScanResult{}, nil, 0)
	telemetry.RecordScan(at.Add(35*time.Millisecond), 35*time.Millisecond, ScanResult{}, nil, 0)
	value := observedScans(t, telemetry)
	if value.SchemaVersion != 1 || len(value.RecentScans) != 2 || value.RecentScans[0].DurationNanos != int64(5*time.Second) ||
		value.RecentScans[1].DurationNanos != int64(35*time.Millisecond) || value.RecentScans[0].CompletedUnixNanos != at.UnixNano() {
		t.Fatalf("complete timings not retained: %+v", value.RecentScans)
	}
	if value.Metrics != telemetry.Render() || !strings.Contains(value.Metrics, "last_scan_duration_seconds 0.035\n") {
		t.Fatal("legacy metrics changed")
	}
}

func TestScanObservationsBoundHistoryAndRetainFailureOutcomes(t *testing.T) {
	telemetry := &Telemetry{}
	if value := observedScans(t, telemetry); value.RecentScans == nil || len(value.RecentScans) != 0 {
		t.Fatal("initial history must be an empty array")
	}
	at := time.Unix(1700000000, 0)
	for i := 1; i <= 40; i++ {
		telemetry.RecordScan(at.Add(time.Duration(i)*time.Second), time.Millisecond, ScanResult{}, nil, 0)
	}
	telemetry.RecordScan(at.Add(41*time.Second), 2*time.Second, ScanResult{}, errors.New("private failure details"), 0)
	value := observedScans(t, telemetry)
	if len(value.RecentScans) != 32 || value.RecentScans[0].Sequence != 10 || value.RecentScans[31].Sequence != 41 ||
		value.RecentScans[31].Result != "failure" {
		t.Fatal("history bounds or failure sequence lost")
	}
	raw, _ := json.Marshal(value)
	if strings.Contains(string(raw), "private") || len(raw) > 16384 {
		t.Fatal("unbounded or sensitive diagnostics")
	}
	value.RecentScans[0].DurationNanos = 0
	if observedScans(t, telemetry).RecentScans[0].DurationNanos == 0 {
		t.Fatal("caller mutated retained observations")
	}
}

func TestScanObservationsAreAtomicDuringConcurrentRecording(t *testing.T) {
	telemetry := &Telemetry{}
	var workers sync.WaitGroup
	workers.Add(1)
	go func() {
		defer workers.Done()
		for i := range 1000 {
			telemetry.RecordScan(time.Unix(1700000000+int64(i), 0), time.Millisecond, ScanResult{}, nil, 0)
		}
	}()
	for range 1000 {
		value := observedScans(t, telemetry)
		if len(value.RecentScans) == 0 {
			continue
		}
		last := value.RecentScans[len(value.RecentScans)-1]
		if !strings.Contains(value.Metrics, fmt.Sprintf("scans_total{result=\"success\"} %d\n", last.Sequence)) {
			t.Error("history and counters came from different observations")
		}
	}
	workers.Wait()
}

func TestScanObservationsRejectMutationMethods(t *testing.T) {
	response := httptest.NewRecorder()
	(&Telemetry{}).Handler().ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/scan-observations", nil))
	if response.Code != http.StatusMethodNotAllowed || response.Header().Get("Allow") != http.MethodGet {
		t.Fatal("diagnostics accepted a mutation method")
	}
}
