package main

import (
	"errors"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/agent"
)

func TestSnapshotPublishBlockReasonProtectsLastGoodData(t *testing.T) {
	tests := []struct {
		name           string
		metadataSynced bool
		result         agent.ScanResult
		want           agentFailureReason
	}{
		{name: "complete", metadataSynced: true},
		{name: "metadata cache pending", metadataSynced: false, want: agentFailureMetadataSync},
		{name: "partial cgroup walk", metadataSynced: true, result: agent.ScanResult{WalkError: errors.New("partial")}, want: agentFailurePartialScan},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := snapshotPublishBlockReason(test.metadataSynced, test.result); got != test.want {
				t.Fatalf("snapshotPublishBlockReason = %q, want %q", got, test.want)
			}
		})
	}
}
