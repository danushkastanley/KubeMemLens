package releasebundle

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestExecutableRequiresThePinnedGoCompiler(t *testing.T) {
	// A real minimal ELF exercises build-info parsing without running a worker or
	// requiring its SDK. Metadata checks do not establish source authenticity.
	//
	// The fixture is built by whichever compiler runs the test, then its recorded
	// version is rewritten, so the result does not depend on the host being at the
	// pin. Rewriting in place needs versions of equal length.
	host := runtime.Version()
	if len(host) != len(pinnedGoVersion) || !strings.HasPrefix(host, "go") {
		t.Skipf("host compiler %s cannot be rewritten to %s in place", host, pinnedGoVersion)
	}
	directory := t.TempDir()
	module := "github.com/danushkastanley/kube-memlens/prototype/trace/worker/cmd/memlens-filecache-worker"
	for name, data := range map[string]string{
		"go.mod":  "module " + module + "\n\ngo " + strings.TrimPrefix(host, "go") + "\n",
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
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte(host)) {
		t.Fatalf("fixture does not record the host compiler %s", host)
	}
	for _, test := range []struct {
		name     string
		version  string
		accepted bool
	}{
		{"pinned compiler", pinnedGoVersion, true},
		{"earlier compiler", "go0" + pinnedGoVersion[len("go1"):], false},
		{"later compiler", "go9" + pinnedGoVersion[len("go1"):], false},
	} {
		t.Run(test.name, func(t *testing.T) {
			executable := bytes.ReplaceAll(raw, []byte(host), []byte(test.version))
			err := ValidateExecutable(executable, "amd64", "memlens-filecache-worker")
			if accepted := err == nil; accepted != test.accepted {
				t.Fatalf("version %s: accepted=%t, want %t", test.version, accepted, test.accepted)
			}
		})
	}
}
