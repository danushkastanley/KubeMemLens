package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/agent"
)

func scanEnvelope(t *testing.T) []byte {
	t.Helper()
	telemetry := &agent.Telemetry{}
	at := time.Unix(1700000000, 123456789)
	telemetry.RecordScan(at, 5*time.Second, agent.ScanResult{}, nil, 0)
	telemetry.RecordScan(at.Add(35*time.Millisecond), 35*time.Millisecond, agent.ScanResult{}, nil, 0)
	response := httptest.NewRecorder()
	telemetry.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/scan-observations", nil))
	if response.Code != 200 {
		t.Fatal("production scan observation failed")
	}
	return response.Body.Bytes()
}

func TestProductionScanHistorySurvivesNumericProjection(t *testing.T) {
	value, err := parseScanObservations(scanEnvelope(t))
	if err != nil || value.Metrics["scanSuccess"] != 2 || len(value.Scans) != 2 ||
		value.Scans[0].DurationNanos != int64(5*time.Second) || value.Scans[1].DurationNanos != int64(35*time.Millisecond) {
		t.Fatal("back-to-back production observations lost", err)
	}
	data, err := json.Marshal(value)
	var child agentObservation
	if err != nil || decodeObservation(data, &child) != nil || validateScanObservations(child) != nil {
		t.Fatal("child numeric projection is not independently valid")
	}
}

func TestScanHistoryRejectsAmbiguousMissingAndChangedFields(t *testing.T) {
	valid := string(scanEnvelope(t))
	for name, invalid := range map[string]string{
		"duplicate": strings.Replace(valid, `"schemaVersion":1`, `"schemaVersion":1,"schemaVersion":1`, 1),
		"unknown":   strings.Replace(valid, `"schemaVersion":1`, `"schemaVersion":1,"private":"data"`, 1),
		"schema":    strings.Replace(valid, `"schemaVersion":1`, `"schemaVersion":2`, 1),
		"sequence":  strings.Replace(valid, `"sequence":2`, `"sequence":3`, 1),
		"clock":     strings.Replace(valid, `"completedUnixNanos":1700000000158456789`, `"completedUnixNanos":1`, 1),
		"duration":  strings.Replace(valid, `"durationNanos":35000000`, `"durationNanos":1`, 1),
		"outcome":   strings.Replace(valid, `"result":"success"`, `"result":"unknown"`, 1),
		"missing":   strings.Replace(valid, `,"result":"success"`, ``, 1),
		"failure":   strings.Replace(valid, `"result":"success"`, `"result":"failure"`, 1),
		"trailing":  valid + "{}",
		"oversized": strings.Repeat(" ", 16385),
	} {
		t.Run(name, func(t *testing.T) {
			if invalid == valid {
				t.Fatal("fixture mutation did not apply")
			}
			if _, err := parseScanObservations([]byte(invalid)); err == nil {
				t.Fatal("invalid scan history accepted")
			}
		})
	}
}

func TestFullHistoryFitsExistingNumericRecordLimit(t *testing.T) {
	telemetry := &agent.Telemetry{}
	for i := range 50 {
		telemetry.RecordScan(time.Unix(1700000000+int64(i), 0), 1800*time.Second, agent.ScanResult{}, nil, 0)
	}
	response := httptest.NewRecorder()
	telemetry.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/scan-observations", nil))
	value, err := parseScanObservations(response.Body.Bytes())
	if err != nil || len(value.Scans) != 32 || value.Scans[0].Sequence != 19 {
		t.Fatal("full history was not bounded", err)
	}
	data, err := json.Marshal(value)
	// Reserve room for clocks, collector counters and observer accounting in
	// the unchanged 8 KiB row and 16 MiB complete-series bounds.
	if err != nil || len(data) > 6500 {
		t.Fatal("history exceeds the observer's existing output bound")
	}
}

func TestAgentObservationUsesFixedRouteOverRealHTTP(t *testing.T) {
	body := scanEnvelope(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/scan-observations" || r.Host != "127.0.0.1:8082" || r.Header.Get("Authorization") != "" {
			t.Error("agent observation route or credentials changed")
		}
		_, _ = w.Write(body)
	}))
	defer server.Close()
	transport := &http.Transport{Proxy: nil, DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 2 * time.Second}
	var out bytes.Buffer
	if err := readAgentObservation(context.Background(), client, &out); err != nil {
		t.Fatal(err)
	}
	var observed agentObservation
	if decodeObservation(out.Bytes(), &observed) != nil || validateScanObservations(observed) != nil || len(observed.Scans) != 2 {
		t.Fatal("real HTTP response lost completed observations")
	}
}

func TestScanArrayBoundsDoNotBroadenCollectorJSON(t *testing.T) {
	if unambiguousJSON([]byte(`{"array":[]}`)) == nil {
		t.Fatal("collector envelope began accepting arrays")
	}
	for _, invalid := range []string{
		`{"recentScans":[{"sequence":1,"Sequence":1}]}`,
		`{"recentScans":[` + strings.Repeat(`{},`, 32) + `{}]}`,
		`{"recentScans":[[[[[1]]]]]}`,
	} {
		if boundedJSON([]byte(invalid), 32) == nil {
			t.Fatal("unbounded or ambiguous scan JSON accepted")
		}
	}
}
