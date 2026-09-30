package releasebundle

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestExecutableRequiresThePinnedGoCompiler(t *testing.T) {
	// A real minimal ELF exercises build-info parsing without running a worker or
	// requiring its SDK. Metadata checks do not establish source authenticity.
	directory := t.TempDir()
	module := "github.com/danushkastanley/kube-memlens/prototype/trace/worker/cmd/memlens-filecache-worker"
	for name, data := range map[string]string{
		"go.mod":  "module " + module + "\n\ngo 1.27.1\n",
		"main.go": "package main\nfunc main() {}\n",
	} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(directory, "worker")
	command := exec.Command("go", "build", "-trimpath", "-buildvcs=false", "-o", path, ".")
	command.Dir = directory
	command.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux", "GOARCH=amd64", "GOWORK=off", "GOTOOLCHAIN=local", "GOPROXY=off")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build fixture: %v: %s", err, output)
	}
	raw, err := os.ReadFile(path)
	if err != nil || ValidateExecutable(raw, "amd64", "memlens-filecache-worker") != nil {
		t.Fatal("pinned compiler fixture refused", err)
	}
	changed := bytes.ReplaceAll(raw, []byte("go1.27.1"), []byte("go1.26.4"))
	if bytes.Equal(raw, changed) {
		t.Fatal("fixture compiler metadata was not changed")
	}
	if ValidateExecutable(changed, "amd64", "memlens-filecache-worker") == nil {
		t.Fatal("different compiler accepted")
	}
}
