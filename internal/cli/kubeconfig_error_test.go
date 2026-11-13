package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommandsNameKubeconfigProblems(t *testing.T) {
	kubeconfig := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(kubeconfig, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"top", "pods"}, {"top", "pods", "--mode=deep"}, {"top", "pods", "--mode=restricted"},
		{"explain", "pod", "api-0"}, {"status"}, {"doctor"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			cmd := NewRootCommand(&stdout, &stderr)
			cmd.SetArgs(append(args, "--kubeconfig", kubeconfig, "--context", "nope"))
			err := cmd.Execute()
			output := stdout.String() + stderr.String()
			if err != nil {
				output += err.Error()
			}
			if !strings.Contains(output, `context "nope" does not exist`) {
				t.Fatalf("output does not name the unknown context:\n%s", output)
			}
			for _, generic := range []string{"evidence is unavailable", "invalid response"} {
				if strings.Contains(output, generic) {
					t.Fatalf("output hides the cause behind %q:\n%s", generic, output)
				}
			}
		})
	}
}
