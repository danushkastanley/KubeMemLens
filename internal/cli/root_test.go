package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestRootHelpShowsPluginNameAndHidesPreV1Flags(t *testing.T) {
	var stdout, stderr bytes.Buffer
	cmd := NewRootCommand(&stdout, &stderr)
	cmd.SetArgs([]string{"--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	help := stdout.String()
	if !strings.Contains(help, "kubectl memlens [command]") || strings.Contains(help, "kubectl-memlens [command]") {
		t.Fatalf("usage does not show the plugin invocation:\n%s", help)
	}
	for _, hidden := range []string{"--connect-mode", "--collector-url", "--collector-namespace", "--collector-service", "--collector-port", "8080"} {
		if strings.Contains(help, hidden) {
			t.Fatalf("help shows pre-v1 option %s:\n%s", hidden, help)
		}
	}
	for _, visible := range []string{"--mode", "--kubeconfig", "--context"} {
		if !strings.Contains(help, visible) {
			t.Fatalf("help is missing %s:\n%s", visible, help)
		}
	}
}

func TestHiddenPreV1FlagsStillParse(t *testing.T) {
	var stdout, stderr bytes.Buffer
	cmd := NewRootCommand(&stdout, &stderr)
	cmd.SetArgs([]string{"--connect-mode=http", "--collector-url=http://127.0.0.1:1", "--collector-port=8080", "version"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("hidden flags rejected: %v", err)
	}
}
