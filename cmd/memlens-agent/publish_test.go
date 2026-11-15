package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/agent"
)

func TestPublishScanCountsAWithheldSnapshot(t *testing.T) {
	tests := []struct {
		name           string
		metadataSynced bool
		result         agent.ScanResult
		wantReason     agentFailureReason
		wantSeries     string
	}{
		{
			name:           "partial cgroup walk",
			metadataSynced: true,
			result:         agent.ScanResult{WalkError: errors.New("partial")},
			wantReason:     agentFailurePartialScan,
			wantSeries:     `kubememlens_agent_snapshots_withheld_total{reason="partial_scan_not_published"} 1`,
		},
		{
			name:       "metadata cache pending",
			wantReason: agentFailureMetadataSync,
			wantSeries: `kubememlens_agent_snapshots_withheld_total{reason="metadata_cache_unsynchronised"} 1`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			telemetry := &agent.Telemetry{}
			published := false
			posted, reason := publishScan(telemetry, test.metadataSynced, test.result, func() error {
				published = true
				return nil
			})
			if posted || published || reason != test.wantReason {
				t.Fatalf("posted=%t published=%t reason=%q, want a withheld snapshot with reason %q", posted, published, reason, test.wantReason)
			}
			metrics := telemetry.Render()
			if !strings.Contains(metrics, test.wantSeries) {
				t.Fatalf("metrics missing %q:\n%s", test.wantSeries, metrics)
			}
			if !strings.Contains(metrics, `kubememlens_agent_snapshot_posts_total{result="failure"} 0`) {
				t.Fatalf("a withheld snapshot was counted as a failed post:\n%s", metrics)
			}
		})
	}
}

func TestPublishScanCountsPostOutcomes(t *testing.T) {
	telemetry := &agent.Telemetry{}
	posted, reason := publishScan(telemetry, true, agent.ScanResult{}, func() error { return nil })
	if !posted || reason != "" {
		t.Fatalf("posted=%t reason=%q, want a posted snapshot", posted, reason)
	}
	posted, reason = publishScan(telemetry, true, agent.ScanResult{}, func() error { return errors.New("collector unavailable") })
	if posted || reason != agentFailureSnapshotPost {
		t.Fatalf("posted=%t reason=%q, want a failed post", posted, reason)
	}
	metrics := telemetry.Render()
	for _, want := range []string{
		`kubememlens_agent_snapshot_posts_total{result="success"} 1`,
		`kubememlens_agent_snapshot_posts_total{result="failure"} 1`,
		`kubememlens_agent_snapshots_withheld_total{reason="partial_scan_not_published"} 0`,
	} {
		if !strings.Contains(metrics, want) {
			t.Fatalf("metrics missing %q:\n%s", want, metrics)
		}
	}
}
