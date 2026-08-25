package main

import "github.com/danushkastanley/kube-memlens/internal/agent"

func snapshotPublishBlockReason(metadataSynced bool, result agent.ScanResult) agentFailureReason {
	if !metadataSynced {
		return agentFailureMetadataSync
	}
	if result.WalkError != nil {
		return agentFailurePartialScan
	}
	return ""
}
