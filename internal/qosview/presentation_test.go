package qosview

import (
	"strings"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/model"
)

func TestPodConfiguredLimitLineSaysWhichParentControlsWereRead(t *testing.T) {
	container := api.ContainerSnapshot{ContainerName: "shared"}
	container.Context.Resources.Pod.Configured.Limit = model.ResourceValue{Known: true, Bytes: 256 << 20}

	before := strings.Join(ContainerLines(container), "\n")
	if !strings.Contains(before, "Pod configured limit: 256Mi; parent cgroup controls are not observed") {
		t.Fatalf("line without a Pod cgroup changed:\n%s", before)
	}
	container.PodCgroup = model.PodCgroupMemory{MaxKnown: true, MaxBytes: 256 << 20}
	after := strings.Join(ContainerLines(container), "\n")
	if !strings.Contains(after, "Pod configured limit: 256Mi; Pod cgroup memory.max: 256Mi; parent memory.min, memory.low and memory.high are not observed") {
		t.Fatalf("line with an observed Pod cgroup limit:\n%s", after)
	}
	if strings.Contains(after, "parent cgroup controls are not observed") {
		t.Fatalf("the line still says no parent control was read:\n%s", after)
	}
}
