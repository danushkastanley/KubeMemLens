package nodeanalysis

import (
	"math"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/capability"
	"github.com/danushkastanley/kube-memlens/internal/nodecontext"
)

func number(value uint64) *uint64     { return &value }
func fraction(value float64) *float64 { return &value }
func testInput() Input {
	now := time.Unix(1800000000, 0).UTC()
	node := &nodecontext.Observation{NodeName: "node-a", NodeUID: "uid-a", ReportedAt: now, Availability: capability.Available,
		Evidence: capability.Envelope{Source: nodecontext.Source, CapturedAt: now, Completeness: capability.Partial},
		Stats: &nodecontext.Stats{StartedAt: now.Add(-2 * time.Hour), Memory: &nodecontext.Memory{CapturedAt: now, UsageBytes: number(1000), PSI: &nodecontext.PSI{}},
			SystemContainers: []nodecontext.SystemContainer{{Category: nodecontext.Kubelet, Memory: &nodecontext.Memory{CapturedAt: now, UsageBytes: number(50)}},
				{Category: nodecontext.Runtime, Memory: &nodecontext.Memory{CapturedAt: now, UsageBytes: number(150)}},
				{Category: nodecontext.Pods, Memory: &nodecontext.Memory{CapturedAt: now, UsageBytes: number(650)}}}},
		Context: &nodecontext.KubernetesContext{CapturedAt: now, CapacityBytes: number(2000), AllocatableBytes: number(1800), MemoryPressure: "False"}}
	return Input{Now: now, NodeName: node.NodeName, NodeUID: node.NodeUID, Current: node, SourceAvailability: capability.Available, Access: ClusterPods,
		Cgroup: CgroupFrame{NodeUID: node.NodeUID, CapturedAt: now, Coverage: Complete, Containers: []Container{{ID: "container-a", Namespace: "team-a", PodName: "app", PodUID: "pod-a", ContainerName: "worker",
			WorkloadKind: "Deployment", WorkloadName: "app", Charge: Charges{Total: 600, Anon: 400, Cache: 100, Shmem: 50, Residual: 50}, CompositionConsistent: true}}},
		Qualification: &Qualification{NodeUID: node.NodeUID, NodeStartedAt: node.Stats.StartedAt, ValidFrom: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour),
			EvidenceSHA256: strings.Repeat("a", 64), UsageDefinition: ChargeInclusive, DisjointSystems: []nodecontext.SystemCategory{nodecontext.Kubelet, nodecontext.Runtime}}}
}

func TestQualifiedEstimatesAndHugepageSeparation(t *testing.T) {
	input := testInput()
	input.Current.Context.Hugepages = []nodecontext.Hugepage{{Resource: "hugepages-2Mi", CapacityBytes: number(512), AllocatableBytes: number(512)}}
	result, err := Analyse(input)
	if err != nil {
		t.Fatal(err)
	}
	if *result.OutsidePods.Bytes != 400 || *result.Unaccounted.Bytes != 200 || *result.ObservedPodCharge != 600 {
		t.Fatalf("incorrect accounting: %#v", result)
	}
	if *result.Facts.Context.CapacityBytes != 2000 || !slices.Contains(result.Caveats, HugepagesSeparate) {
		t.Fatal("hugepages changed ordinary memory facts")
	}
	if result.Unaccounted.Formula == "" || result.Unaccounted.Source == "" || result.Unaccounted.Qualification == "" {
		t.Fatal("derived estimate lost provenance")
	}
	*result.Facts.Context.CapacityBytes = 1
	if *input.Current.Context.CapacityBytes != 2000 {
		t.Fatal("analysis mutated input")
	}
}

func TestEstimatesRejectUnqualifiedIncompatibleOrIncompleteInputs(t *testing.T) {
	for name, change := range map[string]func(*Input){
		"unqualified":    func(i *Input) { i.Qualification = nil },
		"expired":        func(i *Input) { i.Qualification.ExpiresAt = i.Now },
		"wrong boot":     func(i *Input) { i.Qualification.NodeStartedAt = i.Qualification.NodeStartedAt.Add(-time.Second) },
		"stale node":     func(i *Input) { i.Current.Stats.Memory.CapturedAt = i.Now.Add(-time.Minute) },
		"stale cgroup":   func(i *Input) { i.Cgroup.CapturedAt = i.Now.Add(-time.Minute) },
		"skew":           func(i *Input) { i.Cgroup.CapturedAt = i.Now.Add(-6 * time.Second) },
		"missing agent":  func(i *Input) { i.Cgroup.Coverage = Missing; i.Cgroup.Containers = nil },
		"partial agent":  func(i *Input) { i.Cgroup.Coverage = Partial },
		"unmapped":       func(i *Input) { i.Cgroup.Containers[0].PodUID = "" },
		"wrong UID":      func(i *Input) { i.Cgroup.NodeUID = "old-node" },
		"source failure": func(i *Input) { i.SourceAvailability = capability.Forbidden },
		"overflow": func(i *Input) {
			i.Cgroup.Containers[0].Charge.Total = math.MaxUint64
			c := i.Cgroup.Containers[0]
			c.ID = "other"
			i.Cgroup.Containers = append(i.Cgroup.Containers, c)
		},
	} {
		t.Run(name, func(t *testing.T) {
			input := testInput()
			change(&input)
			result, err := Analyse(input)
			if err != nil {
				t.Fatal(err)
			}
			if result.OutsidePods.Bytes != nil || result.Unaccounted.Bytes != nil {
				t.Fatal("unqualified estimate exposed")
			}
		})
	}
}

func TestNegativeGapFloorsAndReducesConfidence(t *testing.T) {
	input := testInput()
	input.Current.Stats.Memory.UsageBytes = number(500)
	result, err := Analyse(input)
	if err != nil {
		t.Fatal(err)
	}
	if *result.OutsidePods.Bytes != 0 || *result.Unaccounted.Bytes != 0 || result.Confidence != Low || !slices.Contains(result.Caveats, Skewed) {
		t.Fatal("negative gap was not qualified")
	}
}

func TestPodAggregateSystemCategoryCannotBeSubtractedTwice(t *testing.T) {
	input := testInput()
	input.Qualification.DisjointSystems = []nodecontext.SystemCategory{nodecontext.Pods}
	result, err := Analyse(input)
	if err != nil {
		t.Fatal(err)
	}
	if result.OutsidePods.Bytes != nil || result.Unaccounted.Bytes != nil {
		t.Fatal("overlapping Pod category accepted as qualification")
	}
}

func TestNodeOnlyAnalysisDoesNotRevealPodDataOrCounts(t *testing.T) {
	input := testInput()
	input.Access = NodeOnly
	result, err := Analyse(input)
	if err != nil {
		t.Fatal(err)
	}
	input.Cgroup.Containers = make([]Container, MaxContainers+1)
	changed, err := Analyse(input)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(result, changed) || result.Rankings != nil || result.Coverage != nil || result.ObservedPodCharge != nil {
		t.Fatal("Node-only output depends on hidden Pods")
	}
}
