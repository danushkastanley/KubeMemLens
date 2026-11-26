package main

import (
	"strings"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/cgroup"
)

// The agent scans every few seconds. An unsupported node must be logged when
// its version is first seen or changes, not on every scan, and a supported
// node must log nothing new.
func TestCgroupVersionNoticeLogsOnlyChanges(t *testing.T) {
	scans := []string{cgroup.VersionV2, cgroup.VersionV2, cgroup.VersionV1, cgroup.VersionV1, cgroup.VersionV2}
	var log strings.Builder
	previous := cgroup.VersionV2
	for _, version := range scans {
		log.WriteString(cgroupVersionNotice("node-a", previous, version))
		previous = version
	}
	want := "cgroup version node=node-a version=v1 supported=false\n" +
		"cgroup version node=node-a version=v2 supported=true\n"
	if log.String() != want {
		t.Fatalf("log =\n%s\nwant\n%s", log.String(), want)
	}
}
