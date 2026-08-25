package main

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type agentFailureReason string

const (
	agentFailureScan          agentFailureReason = "scan_failed"
	agentFailureScanCancelled agentFailureReason = "scan_cancelled"
	agentFailureScanTimeout   agentFailureReason = "scan_timeout"
	agentFailureSnapshotPost  agentFailureReason = "snapshot_post_failed"
)

func boundedScanFailureReason(err error) agentFailureReason {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return agentFailureScanTimeout
	case errors.Is(err, context.Canceled):
		return agentFailureScanCancelled
	default:
		return agentFailureScan
	}
}

func formatAgentFailure(node string, reason agentFailureReason, duration time.Duration) string {
	return fmt.Sprintf("agent operation failed node=%s reason=%s duration=%s\n", node, reason, duration.Round(time.Millisecond))
}

func formatCgroupReadWarning(node string, count int) string {
	if count < 1 {
		count = 1
	}
	return fmt.Sprintf("scan warning node=%s reason=cgroup_read_error count=%d\n", node, count)
}
