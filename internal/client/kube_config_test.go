package client

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/capability"
)

func emptyKubeconfig(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestEvidenceSessionReportsKubeconfigProblems(t *testing.T) {
	for name, test := range map[string]struct {
		opts Options
		want string
	}{
		"empty kubeconfig": {Options{Kubeconfig: emptyKubeconfig(t)}, "no configuration has been provided"},
		"unknown context":  {Options{Kubeconfig: emptyKubeconfig(t), Context: "nope"}, `context "nope" does not exist`},
		"missing file":     {Options{Kubeconfig: filepath.Join(t.TempDir(), "absent")}, "absent"},
	} {
		for _, mode := range []capability.Mode{capability.Auto, capability.Deep, capability.Restricted} {
			t.Run(name+"/"+string(mode), func(t *testing.T) {
				opts := test.opts
				opts.EvidenceMode = mode
				_, err := NewEvidenceSession(context.Background(), opts)
				var configErr *KubeConfigError
				if !errors.As(err, &configErr) {
					t.Fatalf("error = %v, want a kubeconfig error", err)
				}
				message := ConnectionError(opts, "", err).Error()
				if !strings.Contains(message, test.want) || !strings.Contains(message, "--kubeconfig") {
					t.Fatalf("message does not name the cause %q:\n%s", test.want, message)
				}
				for _, generic := range []string{"request-failed", "invalid response", "evidence is unavailable"} {
					if strings.Contains(message, generic) {
						t.Fatalf("message hides the cause behind %q:\n%s", generic, message)
					}
				}
			})
		}
	}
}

func TestSnapshotReaderReportsKubeconfigProblems(t *testing.T) {
	for _, mode := range []ConnectionMode{ConnectionModeKubernetesAPI, ConnectionModeKubeProxy} {
		opts := Options{Mode: mode, Kubeconfig: emptyKubeconfig(t), Context: "nope"}
		_, _, err := NewSnapshotReader(context.Background(), opts)
		if message := ConnectionError(opts, "", err).Error(); !strings.Contains(message, `context "nope" does not exist`) {
			t.Fatalf("%s: %s", mode, message)
		}
	}
}

func TestConnectionErrorExplainsUnusableResponses(t *testing.T) {
	opts := Options{Mode: ConnectionModeKubernetesAPI}
	for name, test := range map[string]struct {
		err  error
		want string
	}{
		"status": {readStatusError("list pods", 410), "list pods: HTTP 410"},
		"decode": {readDecodeError("list pods", fmt.Errorf(`json: unknown field "volumes"`)), `list pods: decode response: json: unknown field "volumes"`},
		"other":  {errors.New("resource name is required"), "resource name is required"},
	} {
		t.Run(name, func(t *testing.T) {
			message := ConnectionError(opts, "", test.err).Error()
			if !strings.Contains(message, "invalid response") || !strings.Contains(message, test.want) {
				t.Fatalf("message = %q, want %q", message, test.want)
			}
		})
	}
}
