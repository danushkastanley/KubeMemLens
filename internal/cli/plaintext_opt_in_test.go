package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/client"
)

const plaintextURL = "http://127.0.0.1:18080"

// runVersion runs a command that needs no cluster, then reports how a command
// that does would connect.
func runVersion(t *testing.T, args ...string) (mode client.ConnectionMode, url, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	root, flags := newRootCommand(&out, &errOut)
	root.SetArgs(append([]string{"version"}, args...))
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	opts := flags.options()
	mode, err := client.ResolveMode(opts)
	if err != nil {
		t.Fatal(err)
	}
	return mode, opts.CollectorURL, errOut.String()
}

// The environment variable used to switch every command to unauthenticated
// HTTP without a word. Plaintext now has to be asked for on the command line.
func TestEnvironmentVariableAloneDoesNotSelectPlaintext(t *testing.T) {
	t.Setenv("MEMLENS_COLLECTOR_URL", plaintextURL)
	mode, url, stderr := runVersion(t)
	if mode != client.ConnectionModeKubernetesAPI || url != "" {
		t.Fatalf("mode = %q, collector URL = %q; want the Kubernetes API and no URL", mode, url)
	}
	if !strings.Contains(stderr, "MEMLENS_COLLECTOR_URL is set but not used") || !strings.Contains(stderr, "--connect-mode=http") {
		t.Fatalf("ignored variable was not explained: %q", stderr)
	}
}

func TestPlaintextIsSelectedExplicitlyAndWarns(t *testing.T) {
	for name, args := range map[string][]string{
		"connect mode with the variable": {"--connect-mode=http"},
		"collector URL flag":             {"--collector-url=" + plaintextURL},
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("MEMLENS_COLLECTOR_URL", plaintextURL)
			mode, url, stderr := runVersion(t, args...)
			if mode != client.ConnectionModeHTTP || url != plaintextURL {
				t.Fatalf("mode = %q, collector URL = %q", mode, url)
			}
			if !strings.Contains(stderr, "without Kubernetes authentication") || !strings.Contains(stderr, plaintextURL) {
				t.Fatalf("plaintext use was not warned about: %q", stderr)
			}
		})
	}
}

func TestDefaultConnectionPrintsNoNotice(t *testing.T) {
	t.Setenv("MEMLENS_COLLECTOR_URL", "")
	mode, _, stderr := runVersion(t)
	if mode != client.ConnectionModeKubernetesAPI || stderr != "" {
		t.Fatalf("mode = %q, stderr = %q", mode, stderr)
	}
}
