package main

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestAgentFailureLogOmitsRawCgroupError(t *testing.T) {
	sentinels := []string{
		"podf2f1de7a-1111-2222-3333-444444444444",
		"0123456789abcdef0123456789abcdef",
		"/host/sys/fs/cgroup/kubepods.slice/tenant-secret.scope",
	}
	raw := fmt.Errorf("parse %s/%s/%s: permission denied", sentinels[2], sentinels[0], sentinels[1])
	line := formatAgentFailure("node-a", boundedScanFailureReason(raw), 1250*time.Millisecond)

	for _, sentinel := range sentinels {
		if strings.Contains(line, sentinel) {
			t.Fatalf("log contains sensitive runtime value %q: %s", sentinel, line)
		}
	}
	for _, expected := range []string{"node=node-a", "reason=scan_failed", "duration=1.25s"} {
		if !strings.Contains(line, expected) {
			t.Fatalf("log missing %q: %s", expected, line)
		}
	}
}

func TestCgroupWarningContainsOnlyBoundedReasonAndCount(t *testing.T) {
	line := formatCgroupReadWarning("node-a", 0)
	if line != "scan warning node=node-a reason=cgroup_read_error count=1\n" {
		t.Fatalf("warning = %q", line)
	}
}
