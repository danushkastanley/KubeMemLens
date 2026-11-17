package main

import "github.com/danushkastanley/kube-memlens/internal/agent"

// publishScan publishes one successful scan and records the outcome. A scan
// that must not be published is counted as withheld, not as a post.
func publishScan(telemetry *agent.Telemetry, metadataSynced bool, result agent.ScanResult, publish func() error) (bool, agentFailureReason) {
	if blockReason := snapshotPublishBlockReason(metadataSynced, result); blockReason != "" {
		telemetry.RecordWithheld(agent.WithheldReason(blockReason))
		return false, blockReason
	}
	err := publish()
	telemetry.RecordPost(err)
	if err != nil {
		return false, agentFailureSnapshotPost
	}
	return true, ""
}
