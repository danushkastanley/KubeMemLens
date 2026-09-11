package incident

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestNoOverwritePublicationIsAtomic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "capture.json")
	var published atomic.Int32
	var writers sync.WaitGroup
	for i := 0; i < 8; i++ {
		writers.Go(func() {
			err := writeDocument(io.Discard, path, false, map[string]string{"value": "complete"})
			if err == nil {
				published.Add(1)
				return
			}
			var exists ExistsError
			if !errors.As(err, &exists) {
				t.Errorf("unexpected publication error: %v", err)
			}
		})
	}
	writers.Wait()
	if published.Load() != 1 {
		t.Fatalf("published %d files", published.Load())
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("private mode: %v %v", info, err)
	}
	if err := writeDocument(io.Discard, path, true, "replacement"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || !bytes.Contains(data, []byte("replacement")) {
		t.Fatal("explicit replacement failed")
	}
}

func TestWriterBoundsBeforePublishingOrReplacing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "capture.json")
	if err := os.WriteFile(path, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	oversized := strings.Repeat("x", int(MaxBytes))
	var output bytes.Buffer
	if err := writeDocument(&output, "-", false, oversized); err == nil || output.Len() != 0 {
		t.Fatalf("oversized stdout partly written: %v", err)
	}
	if err := writeDocument(io.Discard, path, true, oversized); err == nil {
		t.Fatal("oversized replacement accepted")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "original" {
		t.Fatal("failed replacement damaged original")
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(entries) != 1 {
		t.Fatal("staging file leaked")
	}
}

func TestWriterDoesNotFollowDestinationSymlink(t *testing.T) {
	dir := t.TempDir()
	target, link := filepath.Join(dir, "target"), filepath.Join(dir, "link")
	if err := os.WriteFile(target, []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := writeDocument(io.Discard, link, false, "new"); err == nil {
		t.Fatal("symlink replaced without force")
	}
	if err := writeDocument(io.Discard, link, true, "new"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "private" {
		t.Fatal("symlink target overwritten")
	}
}
