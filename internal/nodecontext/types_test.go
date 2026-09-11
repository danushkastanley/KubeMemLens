package nodecontext

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/capability"
)

func TestMemoryRoundTripPreservesUnavailableAndMeasuredZero(t *testing.T) {
	zero := uint64(0)
	original := Memory{CapturedAt: time.Now().UTC(), UsageBytes: &zero}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Memory
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.UsageBytes == nil || *decoded.UsageBytes != 0 || decoded.AvailableBytes != nil || decoded.PSI != nil {
		t.Fatalf("optional values changed: %s", data)
	}
	if !decoded.CapturedAt.Equal(original.CapturedAt) {
		t.Fatal("source timestamp changed")
	}
}

func TestFailureReportCarriesNoReplacementSample(t *testing.T) {
	data, err := json.Marshal(Observation{Availability: capability.Unavailable, Reason: TimedOut})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `"stats"`) {
		t.Fatalf("failure contains a replacement sample: %s", data)
	}
}

func TestMaximumContractEncodingFitsObservationBudget(t *testing.T) {
	maximum := uint64(math.MaxUint64)
	at := time.Date(9999, 12, 31, 23, 59, 59, 999999999, time.UTC)
	psi := PSIData{TotalNanoseconds: maximum, Avg10: 99.99999999999999, Avg60: 99.99999999999999, Avg300: 99.99999999999999}
	memory := &Memory{CapturedAt: at, AvailableBytes: &maximum, UsageBytes: &maximum,
		WorkingSetBytes: &maximum, RSSBytes: &maximum, PageFaults: &maximum,
		MajorPageFaults: &maximum, PSI: &PSI{Some: psi, Full: psi}}
	swap := &Swap{CapturedAt: at, UsageBytes: &maximum, AvailableBytes: &maximum}
	value := Observation{NodeName: strings.Repeat("n", MaxNodeNameBytes), NodeUID: strings.Repeat("u", MaxNodeUIDBytes),
		ReportedAt: at, Availability: capability.Available,
		Evidence: capability.Envelope{Source: Source, APIVersion: "v1alpha1", CapturedAt: at, ReceivedAt: at,
			Scope: capability.NodeScope, Freshness: capability.Fresh, Completeness: capability.Partial, Stability: capability.ImplementationSpecific},
		Stats:   &Stats{StartedAt: at, Provenance: CAdvisor, Memory: memory, Swap: swap},
		Context: &KubernetesContext{CapturedAt: at, CapacityBytes: &maximum, AllocatableBytes: &maximum, MemoryPressure: "Unknown"}}
	for _, category := range []SystemCategory{Kubelet, Runtime, Misc, Pods} {
		value.Stats.SystemContainers = append(value.Stats.SystemContainers, SystemContainer{Category: category, StartedAt: at, Memory: memory, Swap: swap})
	}
	for range MaxHugepages {
		value.Context.Hugepages = append(value.Context.Hugepages, Hugepage{Resource: strings.Repeat("h", MaxResourceBytes), CapacityBytes: &maximum, AllocatableBytes: &maximum})
	}
	for range MaxCaveats {
		value.Evidence.Caveats = append(value.Evidence.Caveats, strings.Repeat("c", MaxCaveatBytes))
	}
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) > MaxObservationBytes {
		t.Fatalf("largest contract fixture is %d bytes, maximum %d", len(data), MaxObservationBytes)
	}
	t.Logf("max-width contract fixture: %d bytes; budget: %d bytes", len(data), MaxObservationBytes)
	t.Logf("5000 latest records at byte ceiling: %d bytes; history ceiling: %d bytes", 5000*MaxObservationBytes, MaxHistoryBytes)
}
