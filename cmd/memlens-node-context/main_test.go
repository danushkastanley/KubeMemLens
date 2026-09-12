package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/nodecontext"
)

func TestVersionNeedsNoCredentials(t *testing.T) {
	var output, diagnostics bytes.Buffer
	if err := run(t.Context(), []string{"--version"}, &output, &diagnostics); err != nil || output.Len() == 0 {
		t.Fatalf("version: %s %v", output.String(), err)
	}
}

func TestDiagnosticOmitsInternalUIDWithoutChangingSource(t *testing.T) {
	report := nodecontext.Observation{NodeName: "node-a", NodeUID: "private-node-uid"}
	data, err := diagnosticBytes(report)
	if err != nil || strings.Contains(string(data), "private-node-uid") || !strings.Contains(string(data), `"redacted":true`) {
		t.Fatalf("diagnostic disclosure: %s %v", data, err)
	}
	if report.NodeUID != "private-node-uid" {
		t.Fatal("source identity changed")
	}
}

func TestCollectionRequiresExplicitSelectionAndCredentials(t *testing.T) {
	for _, args := range [][]string{{}, {"--once"}, {"--once", "--node-name=node-a"}, {"unexpected"}} {
		var output, diagnostics bytes.Buffer
		if err := run(t.Context(), args, &output, &diagnostics); err == nil || output.Len() != 0 {
			t.Fatalf("unexpected collection for %v", args)
		}
	}
}

func TestPrivateOutputNeverOverwrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "observation.json")
	if err := writePrivate(path, []byte("first")); err != nil {
		t.Fatal(err)
	}
	if err := writePrivate(path, []byte("replacement")); err == nil {
		t.Fatal("overwrote existing output")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "first" {
		t.Fatal("existing data changed")
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("output is not private")
	}
}

func TestLocalMemoryControllerPreflight(t *testing.T) {
	for _, test := range []struct {
		input string
		want  bool
	}{
		{"0::/\n", true}, {"0::/kubepods/pod-a\n", true},
		{"2:cpu:/\n0::/\n", true}, {"2:memory:/\n0::/\n", false},
		{"5:cpu,memory:/\n", false}, {"", false}, {"invalid", false},
	} {
		if got := memoryUsesCgroupV2(test.input); got != test.want {
			t.Fatalf("membership %q = %v", strings.TrimSpace(test.input), got)
		}
	}
}

func TestPublishingAndDiagnosticModesAreExclusive(t *testing.T) {
	for _, args := range [][]string{{"--publish", "--once"}, {"--publish", "--output=private.json"}, {"--publish", "--metrics-output=private.prom"}} {
		var output, diagnostics bytes.Buffer
		if err := run(t.Context(), args, &output, &diagnostics); err == nil {
			t.Fatal("mixed producer/diagnostic mode accepted")
		}
	}
}
