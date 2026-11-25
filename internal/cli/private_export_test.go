package cli

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/client"
)

func runExport(t *testing.T, terminal bool, args ...string) error {
	t.Helper()
	commands := &incidentCommands{
		// The connection is never reached when the export is refused, and
		// fails at once otherwise: this address has no kubeconfig.
		options:  func() client.Options { return client.Options{Kubeconfig: "/nonexistent/kubeconfig"} },
		terminal: func(io.Writer) bool { return terminal },
	}
	cmd := commands.export()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(append([]string{"session-id"}, args...))
	return cmd.Execute()
}

func refused(err error) bool {
	return err != nil && strings.Contains(err.Error(), "terminal")
}

// A full export holds private annotations, identities and capture bytes. It
// must not be printed to a screen because --output was left at its default.
func TestPrivateExportIsNotWrittenToATerminal(t *testing.T) {
	if err := runExport(t, true, "--include-sensitive"); !refused(err) {
		t.Fatalf("private export to a terminal: err = %v, want a refusal that names the terminal", err)
	}
	for name, err := range map[string]error{
		"private export to a pipe":           runExport(t, false, "--include-sensitive"),
		"private export to a file":           runExport(t, true, "--include-sensitive", "--output", "export.json"),
		"sanitised export to a terminal":     runExport(t, true),
		"sanitised export to a terminal, -o": runExport(t, true, "--output", "-"),
	} {
		if refused(err) {
			t.Fatalf("%s was refused: %v", name, err)
		}
	}
}
