package incident

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/traceclient"
	"github.com/danushkastanley/kube-memlens/internal/tracereport"
)

func TestTraceExportUsesPrivateAtomicPublication(t *testing.T) {
	doc, err := tracereport.New(traceclient.Snapshot{State: traceclient.StateCancelled, Cleanup: traceclient.CleanupNotRequested}, "test", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "report.json")
	var stdout bytes.Buffer
	if err := WriteTrace(&stdout, path, false, doc); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatal("export permissions", err)
	}
	expected, _ := doc.Bytes()
	data, _ := os.ReadFile(path)
	if !bytes.Equal(data, expected) || stdout.Len() != 0 {
		t.Fatal("incorrect publication")
	}
	if err := WriteTrace(&stdout, path, false, doc); err == nil {
		t.Fatal("overwrote existing file")
	}
	link := filepath.Join(dir, "link.json")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if err := WriteTrace(&stdout, link, false, doc); err == nil {
		t.Fatal("followed symlink")
	}
	if err := WriteTrace(&stdout, "-", false, doc); err != nil || !bytes.Equal(stdout.Bytes(), expected) {
		t.Fatal("stdout publication", err)
	}
}
