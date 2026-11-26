package cli

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/client"
	"github.com/danushkastanley/kube-memlens/internal/collector"
)

func TestCollectorHistorySummaryNamesARecentStartUntilTheWindowIsFull(t *testing.T) {
	started := time.Date(2026, 11, 26, 10, 5, 0, 0, time.UTC)
	for _, tc := range []struct {
		name     string
		received time.Time
		want     string
	}{
		{"no snapshot yet", time.Time{}, "collector started at 2026-11-26T10:05:00Z and holds at most 0s of Pod history; nothing from before then is kept"},
		{"four minutes in", started.Add(4*time.Minute + 10*time.Second), "collector started at 2026-11-26T10:05:00Z and holds at most 4m10s of Pod history; nothing from before then is kept"},
		{"one second short of the window", started.Add(15*time.Minute - time.Second), "holds at most 14m59s of Pod history"},
		{"window full", started.Add(15 * time.Minute), "collector has run since 2026-11-26T10:05:00Z"},
		{"a day later", started.Add(24 * time.Hour), "collector has run since 2026-11-26T10:05:00Z"},
	} {
		got := collectorHistorySummary(api.CollectorReliability{StartedAt: started, LastReceivedAt: tc.received})
		if !strings.Contains(got, tc.want) {
			t.Errorf("%s: summary %q does not contain %q", tc.name, got, tc.want)
		}
		if recent := strings.Contains(got, "incident capture"); recent == strings.HasPrefix(got, "collector has run since") {
			t.Errorf("%s: summary %q must either point at a capture or say the collector has run since its start, not both or neither", tc.name, got)
		}
	}
}

// doctor --strict is the readiness gate in the kind harness and the release
// consumer check, within two minutes of install, and it fails on any warning.
// A collector that has just started must tell the operator so as a pass.
func TestDoctorNamesAJustStartedCollectorWithoutWarning(t *testing.T) {
	store := collector.NewStore()
	store.ReplaceNodeSnapshot(api.AgentSnapshot{NodeName: "node-a", CapturedAt: time.Now().UTC()})
	server := httptest.NewServer(collector.NewReadHandlerWithOptions(store, collector.DefaultHandlerOptions(time.Minute)))
	defer server.Close()

	output := &bytes.Buffer{}
	cmd := newDoctorCommand(func() client.Options {
		return client.Options{Mode: client.ConnectionModeHTTP, CollectorURL: server.URL}
	})
	cmd.SetOut(output)
	cmd.SetArgs([]string{"--output", "json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("doctor failed: %v\n%s", err, output.String())
	}
	var report doctorReport
	if err := json.Unmarshal(output.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	for _, check := range report.Checks {
		if check.Name != "collector history" {
			continue
		}
		if check.Status != "pass" || !strings.HasPrefix(check.Summary, "collector started at ") {
			t.Fatalf("collector history check on a collector that has just started is %s %q, want a pass that names the start", check.Status, check.Summary)
		}
		return
	}
	t.Fatalf("doctor reported no collector history check:\n%s", output.String())
}
