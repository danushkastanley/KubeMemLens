package targetfs

import (
	"context"
	"os"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/trace"
)

func TestWorkerDescriptorRejectsWrongFilesystemAndClosedFile(t *testing.T) {
	file, err := os.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	target := workload().Target
	target.CgroupID = 123
	if VerifyWorkerDescriptor(context.Background(), file, target) == nil {
		t.Fatal("non-cgroup directory accepted")
	}
	if _, err := ReadWorkerMemoryStat(context.Background(), file, target); err == nil {
		t.Fatal("unverified descriptor used for statistics")
	}
	file.Close()
	if VerifyWorkerDescriptor(context.Background(), file, target) == nil {
		t.Fatal("closed descriptor accepted")
	}
	if VerifyWorkerDescriptor(context.Background(), nil, trace.TargetIdentity{}) == nil {
		t.Fatal("missing target accepted")
	}
}
