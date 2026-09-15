package workerinstall

import (
	"github.com/danushkastanley/kube-memlens/prototype/trace/internal/testworker"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func acceptedSelf(t *testing.T) (*Policy, string) {
	t.Helper()
	path, err := testworker.StaticBinary()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	config := configuration(t)
	config.Engine.Workers[runtime.GOARCH] = sum(data)
	signEngine(t, &config)
	policy, err := Parse(encoded(t, config))
	if err != nil {
		t.Fatal(err)
	}
	return policy, path
}

func TestSealedExecutableFixture(t *testing.T) {
	if os.Getenv("KML_SEALED_EXEC_FIXTURE") == "1" {
		os.Exit(0)
	}
}

func TestAcceptedExecutableRunsFromImmutableDescriptor(t *testing.T) {
	policy, path := acceptedSelf(t)
	image, err := policy.Executable(path, runtime.GOARCH)
	if err != nil {
		t.Fatal(err)
	}
	defer image.Close()
	if _, err := image.WriteAt([]byte("changed"), 0); err == nil {
		t.Fatal("sealed image remained writable")
	}
	if image.Truncate(0) == nil || image.Chmod(0600) == nil {
		t.Fatal("sealed image shape or execution permission could change")
	}
	seals, err := unix.FcntlInt(image.Fd(), unix.F_GET_SEALS, 0)
	if err != nil || seals&executableSeals != executableSeals {
		t.Fatal("missing executable seals")
	}
	cmd := exec.Command("/proc/self/fd/3", "-test.run=^TestSealedExecutableFixture$")
	cmd.ExtraFiles = []*os.File{image}
	cmd.Env = []string{"KML_SEALED_EXEC_FIXTURE=1"}
	if err := cmd.Run(); err != nil {
		t.Fatal("sealed executable did not run")
	}
}

func TestExecutableRejectsUnacceptedOrMismatchedImages(t *testing.T) {
	policy, path := acceptedSelf(t)
	if _, err := policy.Executable(path, "unsupported"); err == nil {
		t.Fatal("unaccepted architecture permitted")
	}
	config := configuration(t)
	config.Engine.Workers[runtime.GOARCH] = strings.Repeat("f", 64)
	signEngine(t, &config)
	wrong, err := Parse(encoded(t, config))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wrong.Executable(path, runtime.GOARCH); err == nil {
		t.Fatal("wrong executable digest accepted")
	}
	other := "amd64"
	if runtime.GOARCH == other {
		other = "arm64"
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	config.Engine.Workers[other] = sum(data)
	signEngine(t, &config)
	wrong, err = Parse(encoded(t, config))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wrong.Executable(path, other); err == nil {
		t.Fatal("wrong ELF architecture accepted")
	}
	link := filepath.Join(t.TempDir(), "worker")
	if os.Symlink(path, link) != nil {
		t.Fatal("fixture symlink failed")
	}
	if _, err := policy.Executable(link, runtime.GOARCH); err == nil {
		t.Fatal("executable symlink accepted")
	}
}

func TestExecutableSnapshotSurvivesSourceReplacement(t *testing.T) {
	policy, path := acceptedSelf(t)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	copyPath := filepath.Join(t.TempDir(), "worker")
	if os.WriteFile(copyPath, data, 0700) != nil {
		t.Fatal("fixture copy failed")
	}
	image, err := policy.Executable(copyPath, runtime.GOARCH)
	if err != nil {
		t.Fatal(err)
	}
	defer image.Close()
	if os.WriteFile(copyPath, []byte("changed"), 0700) != nil {
		t.Fatal("fixture replacement failed")
	}
	cmd := exec.Command("/proc/self/fd/3", "-test.run=^TestSealedExecutableFixture$")
	cmd.ExtraFiles = []*os.File{image}
	cmd.Env = []string{"KML_SEALED_EXEC_FIXTURE=1"}
	if err := cmd.Run(); err != nil {
		t.Fatal("source mutation affected accepted executable")
	}
}
