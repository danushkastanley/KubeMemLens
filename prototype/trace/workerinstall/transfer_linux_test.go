//go:build linux && (amd64 || arm64)

package workerinstall

import (
	"github.com/danushkastanley/kube-memlens/prototype/trace/internal/testworker"
	"github.com/danushkastanley/kube-memlens/prototype/trace/workercontainment"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"testing"
)

func TestPolicyDescriptorIsImmutableNonExecutableAndConcurrent(t *testing.T) {
	policy, err := Parse(encoded(t, configuration(t)))
	if err != nil {
		t.Fatal(err)
	}
	file, err := policy.Descriptor()
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := file.WriteAt([]byte("changed"), 0); err == nil {
		t.Fatal("policy remained writable")
	}
	if file.Truncate(0) == nil || file.Chmod(0500) == nil {
		t.Fatal("policy could be resized or made executable")
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 10 {
				got, err := ReadDescriptor(file)
				if err != nil || got.EngineDigest() != policy.EngineDigest() {
					t.Error("positional policy transfer failed")
				}
			}
		})
	}
	wg.Wait()
	regular, err := os.CreateTemp(t.TempDir(), "policy")
	if err != nil {
		t.Fatal(err)
	}
	defer regular.Close()
	if _, err := regular.Write(encoded(t, configuration(t))); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadDescriptor(regular); err == nil {
		t.Fatal("unsealed policy descriptor accepted")
	}
}

func TestRunningImageFixture(t *testing.T) {
	if os.Getenv("KML_RUNNING_IMAGE_FIXTURE") != "1" {
		return
	}
	image, policyFile := os.NewFile(3, "image"), os.NewFile(4, "policy")
	if testworker.Baseline() != nil || workercontainment.Restrict() != nil || testworker.CheckBaseline() != nil {
		os.Exit(21)
	}
	policy, err := ReadDescriptor(policyFile)
	if err != nil || policy.VerifyRunning(image) != nil {
		os.Exit(20)
	}
	os.Exit(0)
}

func TestWorkerChecksItsOwnAcceptedImage(t *testing.T) {
	policy, path := acceptedSelf(t)
	image, err := policy.Executable(path, runtime.GOARCH)
	if err != nil {
		t.Fatal(err)
	}
	defer image.Close()
	file, err := policy.Descriptor()
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if policy.VerifyRunning(image) == nil {
		t.Fatal("unrelated valid image treated as running process")
	}
	cmd := exec.Command("/proc/self/fd/3", "-test.run=^TestRunningImageFixture$")
	cmd.ExtraFiles = []*os.File{image, file}
	cmd.Env = []string{"KML_RUNNING_IMAGE_FIXTURE=1"}
	if err := cmd.Run(); err != nil {
		t.Fatal("worker could not verify its own sealed image and policy")
	}
}
