package explain

import (
	"strings"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/model"
)

// A container without its own limit under a Pod-level limit: once the Pod
// cgroup's memory.max is read, the correlation states it instead of saying the
// container only "may" inherit a limit. Parent memory.high is still not read.
func TestMemoryQoSStatesTheObservedPodCgroupLimit(t *testing.T) {
	container := qosFixture()
	container.Context.MemoryLimitKnown, container.Context.MemoryLimitBytes = false, 0
	container.Context.Resources.Pod.Configured.Limit = model.ResourceValue{Known: true, Bytes: 256 << 20}

	before := strings.Join(InterpretMemoryQoS(container).Correlations, "\n")
	if !strings.Contains(before, "The container may inherit Pod limits") {
		t.Fatalf("correlations without a Pod cgroup changed:\n%s", before)
	}
	container.PodCgroup = model.PodCgroupMemory{MaxKnown: true, MaxBytes: 256 << 20}
	after := strings.Join(InterpretMemoryQoS(container).Correlations, "\n")
	if !strings.Contains(after, "The Pod cgroup's memory.max of 256Mi also applies to this container; parent memory.high is not collected") || strings.Contains(after, "may inherit") {
		t.Fatalf("correlations with an observed Pod cgroup limit:\n%s", after)
	}
}
