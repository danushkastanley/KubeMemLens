package incident

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// Some filesystems have no hard links: several network and FUSE mounts, and
// exFAT. Publishing without --overwrite must still work there and must still
// never replace an existing destination.
func noHardLinks(string, string) error {
	return &os.LinkError{Op: "link", Err: syscall.EPERM}
}

func publishWithoutLinks(path, content string) error {
	return publishIncidentFile(io.Discard, path, false, func(w io.Writer) error {
		_, err := io.WriteString(w, content)
		return err
	}, noHardLinks)
}

func TestPublishingWithoutHardLinksCreatesButNeverReplaces(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "capture.json")
	if err := publishWithoutLinks(path, "first"); err != nil {
		t.Fatalf("publish on a filesystem without hard links: %v", err)
	}
	data, err := os.ReadFile(path)
	info, statErr := os.Stat(path)
	if err != nil || string(data) != "first" || statErr != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("published file: content %q, mode %v, errors %v %v", data, info, err, statErr)
	}

	var exists ExistsError
	if err := publishWithoutLinks(path, "second"); !errors.As(err, &exists) {
		t.Fatalf("existing destination: err = %v, want ExistsError", err)
	}
	if data, _ := os.ReadFile(path); string(data) != "first" {
		t.Fatalf("existing destination was replaced with %q", data)
	}

	target, link := filepath.Join(dir, "target"), filepath.Join(dir, "link")
	if err := os.WriteFile(target, []byte("private"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skip("symbolic links unavailable")
	}
	if err := publishWithoutLinks(link, "through the link"); !errors.As(err, &exists) {
		t.Fatalf("symlink destination: err = %v, want ExistsError", err)
	}
	if data, _ := os.ReadFile(target); string(data) != "private" {
		t.Fatalf("symlink target was overwritten with %q", data)
	}
	assertOnlyFiles(t, dir, "capture.json", "target", "link")
}

// Ctrl+C cancels the read that feeds the file. Nothing may be left behind.
func TestInterruptedWriteLeavesNoStagedFile(t *testing.T) {
	dir := t.TempDir()
	err := writeIncidentFile(io.Discard, filepath.Join(dir, "capture.json"), false, func(w io.Writer) error {
		_, _ = io.WriteString(w, "partial")
		return context.Canceled
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want the cancellation", err)
	}
	assertOnlyFiles(t, dir)
}

func assertOnlyFiles(t *testing.T, dir string, want ...string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, entry := range entries {
		names[entry.Name()] = true
	}
	for _, name := range want {
		if !names[name] {
			t.Fatalf("%s is missing from %v", name, names)
		}
		delete(names, name)
	}
	if len(names) != 0 {
		t.Fatalf("left behind: %v", names)
	}
}
