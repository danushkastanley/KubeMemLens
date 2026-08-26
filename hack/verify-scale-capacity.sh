#!/usr/bin/env bash

set -Eeuo pipefail

go test ./internal/collector -count=1 \
  -run '^(TestContainerPagesServeTenThousandRealisticRecordsWithinResponseBound|TestSnapshotEndpointReportsStoreCapacity|TestStoreEnforcesNodeAndContainerCapacity|TestHistoryReliabilityRecoversAfterCapacityLossLeavesWindow)$'
go test ./internal/extension -count=1 \
  -run '^(TestCoordinatorStoreFailureDoesNotAdvanceSequence|TestReadHandlerReportsScopedHistoryCapacityLoss)$'
go test ./internal/tui -count=1 \
  -run '^TestStaleAndPartialCollectorStatesRemainDistinct$'

echo "scale capacity and degraded-state checks passed"
