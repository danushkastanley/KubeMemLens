package nodestats

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/capability"
	"github.com/danushkastanley/kube-memlens/internal/memorytopology"
)

type topologyFunc func(context.Context, string, string) (memorytopology.Observation, error)

func (f topologyFunc) Read(ctx context.Context, name, uid string) (memorytopology.Observation, error) {
	return f(ctx, name, uid)
}
func TestTopologyFailureDoesNotInvalidateNodeOrVolumes(t *testing.T) {
	source, count := volumeHarness(t, withVolumePods(`[]`), VolumeStatsEnabled)
	source.opts.Topology = topologyFunc(func(ctx context.Context, name, uid string) (memorytopology.Observation, error) {
		if name != "node-a" || uid != "node-uid-a" {
			t.Fatal("unverified identity sent to source")
		}
		return memorytopology.Observation{}, errors.New("source failure")
	})
	sample, err := source.ReadSample(t.Context())
	if err != nil || sample.Node.Availability != capability.Available || sample.Volumes == nil || sample.Topology == nil || count.Load() != 1 {
		t.Fatalf("optional failure erased ordinary evidence: %+v %v", sample, err)
	}
	if sample.Topology.Pools.Reason != memorytopology.SourceFailed || !sample.Topology.ReportedAt.Equal(sample.Node.ReportedAt) {
		t.Fatal("source failure contract changed")
	}
	data, err := json.Marshal(sample.Node)
	if err != nil || strings.Contains(string(data), "linux-hugetlb-sysfs") {
		t.Fatal("ordinary Node contract gained topology")
	}
}
func TestTopologyDisabledOmitsEvidence(t *testing.T) {
	source, _ := volumeHarness(t, summaryBody(), VolumeStatsDisabled)
	sample, err := source.ReadSample(t.Context())
	if err != nil || sample.Topology != nil {
		t.Fatal("default collection gained topology")
	}
}
