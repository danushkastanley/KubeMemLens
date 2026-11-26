package main

import (
	"bytes"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/traceaudit"
)

func TestAuditKeyLoadingRequiresOneBoundedRegularFile(t *testing.T) {
	const expected = "72cd6e8422c407fb6d098690f1130b7ded7ec2f7f5e1d30bd9d521f015363793"
	dir := t.TempDir()
	key := filepath.Join(dir, "reference.key")
	if err := os.WriteFile(key, bytes.Repeat([]byte{1}, 32), 0o400); err != nil {
		t.Fatal(err)
	}
	original, err := loadAuditReferences(key, expected)
	if err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "projected.key")
	if err := os.Symlink(key, link); err != nil {
		t.Fatal(err)
	}
	if _, err := loadAuditReferences(key, ""); err != traceaudit.ErrInvalid {
		t.Fatal("missing pin accepted")
	}
	if _, err := loadAuditReferences(key, string(bytes.Repeat([]byte{'0'}, 64))); err != traceaudit.ErrInvalid {
		t.Fatal("changed key accepted")
	}
	projected, err := loadAuditReferences(link, expected)
	if err != nil || projected.KeyID() != original.KeyID() {
		t.Fatal("projected Secret symlink rejected", err)
	}
	for _, size := range []int{0, 31, 33, 4096} {
		path := filepath.Join(dir, "invalid.key")
		if err := os.WriteFile(path, make([]byte, size), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := loadAuditReferences(path, expected); err != traceaudit.ErrInvalid {
			t.Fatal("invalid key accepted")
		}
	}
	pipe := filepath.Join(dir, "pipe")
	if err := syscall.Mkfifo(pipe, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{pipe, dir, filepath.Join(dir, "absent")} {
		if _, err := loadAuditReferences(path, expected); err != traceaudit.ErrInvalid {
			t.Fatal("non-file key accepted")
		}
	}
}
