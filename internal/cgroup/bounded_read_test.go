package cgroup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOversizedCgroupFileIsAnErrorNotATruncatedRead(t *testing.T) {
	dir := t.TempDir()
	writeCgroupFiles(t, dir, 100, 60)
	// Valid lines all the way through: a truncated read would parse cleanly
	// and report a prefix of the file as the whole.
	oversized := strings.Repeat("anon 1\n", maxCgroupFileBytes/len("anon 1\n")+1)
	if err := os.WriteFile(filepath.Join(dir, "memory.stat"), []byte(oversized), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseDirectory("oversized", dir); err == nil || !strings.Contains(err.Error(), "memory.stat is larger than") {
		t.Fatalf("oversized memory.stat: err = %v", err)
	}
}

func TestCgroupFilesAreReadOnlyInsideTheirDirectory(t *testing.T) {
	outside := t.TempDir()
	writeCgroupFiles(t, outside, 999, 999)
	dir := t.TempDir()
	writeCgroupFiles(t, dir, 100, 60)
	// A cgroup directory never holds a link, but a mount the agent is pointed
	// at by mistake could. The read must not follow it out of the directory.
	if err := os.Remove(filepath.Join(dir, "memory.current")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "memory.current"), filepath.Join(dir, "memory.current")); err != nil {
		t.Skip("symbolic links unavailable")
	}
	if breakdown, err := ParseDirectory("escaping", dir); err == nil {
		t.Fatalf("read escaped its directory and reported %d bytes", breakdown.TotalBytes)
	}
}

func TestMissingCgroupDirectoryIsStillRecognisedAsGone(t *testing.T) {
	_, err := ParseDirectory("gone", filepath.Join(t.TempDir(), "removed"))
	if !cgroupGone(err) {
		t.Fatalf("a removed cgroup directory is not treated as gone: %v", err)
	}
}
