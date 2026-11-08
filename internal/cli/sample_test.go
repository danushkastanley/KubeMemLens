package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runSampleCommand(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	cmd := NewRootCommand(&stdout, &stderr)
	cmd.SetArgs(append([]string{"sample"}, args...))
	err := cmd.Execute()
	return stdout.String(), err
}

// The released binary runs from arbitrary directories, so samples must not
// depend on a source checkout.
func TestSampleCommandsUseBundledDataOutsideCheckout(t *testing.T) {
	t.Setenv("KUBEMEMLENS_SAMPLE_ROOT", "")
	t.Chdir(t.TempDir())

	list, err := runSampleCommand(t, "list")
	if err != nil {
		t.Fatalf("sample list: %v", err)
	}
	for _, name := range preferredSampleOrder {
		if !strings.Contains(list, "  "+name+"\n") {
			t.Fatalf("sample list missing %s:\n%s", name, list)
		}
	}

	explanation, err := runSampleCommand(t, "explain", "cache-heavy")
	if err != nil {
		t.Fatalf("sample explain: %v", err)
	}
	if !strings.Contains(explanation, "Sample: cache-heavy") || !strings.Contains(explanation, "cache-heavy\nSeverity:") {
		t.Fatalf("unexpected explanation:\n%s", explanation)
	}

	top, err := runSampleCommand(t, "top")
	if err != nil {
		t.Fatalf("sample top: %v", err)
	}
	if !strings.Contains(top, "tmpfs-heavy") {
		t.Fatalf("sample top missing rows:\n%s", top)
	}
}

func TestSampleRootOverride(t *testing.T) {
	root := t.TempDir()
	sample := filepath.Join(root, "custom")
	if err := os.Mkdir(sample, 0o700); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"memory.current": "1048576\n",
		"memory.stat":    "anon 524288\nfile 262144\nshmem 0\nactive_file 131072\ninactive_file 131072\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(sample, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("KUBEMEMLENS_SAMPLE_ROOT", root)

	list, err := runSampleCommand(t, "list")
	if err != nil || strings.Contains(list, "cache-heavy") || !strings.Contains(list, "  custom\n") {
		t.Fatalf("override list = %q, %v", list, err)
	}
	if _, err := runSampleCommand(t, "explain", "custom"); err != nil {
		t.Fatalf("override explain: %v", err)
	}
}

func TestSampleExplainRejectsUnknownAndUnsafeNames(t *testing.T) {
	t.Setenv("KUBEMEMLENS_SAMPLE_ROOT", "")
	for name, want := range map[string]string{
		"missing":       `unknown sample "missing"`,
		"../etc":        `invalid sample name "../etc"`,
		"cache-heavy/x": `invalid sample name "cache-heavy/x"`,
		".":             `invalid sample name "."`,
	} {
		if _, err := runSampleCommand(t, "explain", name); err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("explain %q error = %v, want %q", name, err, want)
		}
	}
}
