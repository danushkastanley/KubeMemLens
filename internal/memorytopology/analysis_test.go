package memorytopology

import (
	"encoding/json"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/capability"
)

func value(n uint64) *uint64 { return &n }
func sample() Observation {
	now := time.Date(2026, 9, 29, 1, 0, 0, 0, time.UTC)
	placement := func() *Placement {
		return &Placement{HitPages: value(10), MissPages: value(0), ForeignPages: value(0), LocalPages: value(10), OtherPages: value(0), InterleavePages: value(0)}
	}
	return Observation{SchemaVersion: 1, NodeName: "node-a", NodeUID: "node-uid", ReportedAt: now,
		NUMA:   Section[NUMANode]{Source: NUMASysfs, Availability: capability.Available, Completeness: capability.Complete, CapturedAt: now, Items: []NUMANode{{ID: 0, TotalBytes: value(1000 << 20), FreeBytes: value(800 << 20), Placement: placement(), Pools: []HugePool{}}, {ID: 1, TotalBytes: value(1000 << 20), FreeBytes: value(400 << 20), Placement: placement(), Pools: []HugePool{}}}},
		Pools:  Section[HugePool]{Source: HugeTLBSysfs, Availability: capability.Available, Completeness: capability.Complete, CapturedAt: now, Items: []HugePool{{PageSizeBytes: 2 << 20, TotalPages: value(8), FreePages: value(7), ReservedPages: value(1), SurplusPages: value(2)}}},
		Cgroup: Section[HugeCgroup]{Source: HugeTLBCgroup, Availability: capability.Available, Completeness: capability.Complete, CapturedAt: now, Items: []HugeCgroup{{PageSizeBytes: 2 << 20, CurrentBytes: value(2 << 20), Limit: Limit{Unlimited: true}, LimitFailures: value(1), NUMATotalBytes: value(2 << 20), NUMA: []DomainBytes{{ID: 0, Bytes: 2 << 20}, {ID: 1, Bytes: 0}}}}}}
}

func TestTopologySeparatesPoolReservationUsageSurplusAndFailures(t *testing.T) {
	o := sample()
	r, err := Analyse(o, o.ReportedAt)
	if err != nil {
		t.Fatal(err)
	}
	p := r.Pools[0]
	if *p.PoolBytes != 16<<20 || *p.InUseBytes != 2<<20 || *p.ReservedBytes != 2<<20 || *p.FreeUnreservedBytes != 12<<20 {
		t.Fatal("overlapping hugepage quantities were combined", p)
	}
	if *r.Observation.Cgroup.Items[0].LimitFailures != 1 {
		t.Fatal("failure counter changed")
	}
	if r.NUMA.State != "uneven-nonfree" || *r.NUMA.MostOccupiedID != 1 {
		t.Fatalf("missing NUMA distribution: %+v", r.NUMA)
	}
	if *r.NUMA.FractionSpread != .4 || *r.NUMA.EstimatedExcessBytes != 400<<20 {
		t.Fatal("incorrect normalisation", r.NUMA)
	}
	*r.Observation.Pools.Items[0].FreePages = 0
	*r.Observation.NUMA.Items[0].TotalBytes = 0
	if *o.Pools.Items[0].FreePages != 7 || *o.NUMA.Items[0].TotalBytes != 1000<<20 {
		t.Fatal("analysis mutated source ownership")
	}
}

func TestTopologyNeverCallsMissingOrStaleNUMABalanced(t *testing.T) {
	for _, scenario := range []string{"missing field", "single domain", "memoryless", "stale", "old retained", "hotplug", "inconsistent", "unsupported"} {
		t.Run(scenario, func(t *testing.T) {
			o := sample()
			now := o.ReportedAt
			want := "partial"
			switch scenario {
			case "missing field":
				o.NUMA.Items[1].FreeBytes = nil
				o.NUMA.Completeness = capability.Partial
				o.NUMA.Reason = PartialFields
			case "single domain":
				o.NUMA.Items = o.NUMA.Items[:1]
				want = "insufficient-memory-domains"
			case "memoryless":
				o.NUMA.Items[1].TotalBytes = value(0)
				o.NUMA.Items[1].FreeBytes = value(0)
				want = "insufficient-memory-domains"
			case "stale":
				now = now.Add(time.Minute)
				want = "stale"
			case "old retained":
				now = now.Add(48 * time.Hour)
				want = "stale"
			case "hotplug":
				o.NUMA.Completeness = capability.Partial
				o.NUMA.Reason = SourceChanged
				want = "source-changed"
			case "inconsistent":
				o.NUMA.Items[1].FreeBytes = value(1001 << 20)
				o.NUMA.Completeness = capability.Partial
				o.NUMA.Reason = PartialFields
			case "unsupported":
				o.NUMA = Section[NUMANode]{Source: NUMASysfs, Availability: capability.Unsupported, Completeness: capability.Partial, Reason: SourceAbsent}
				want = "unsupported"
			}
			r, err := Analyse(o, now)
			if err != nil {
				t.Fatal(err)
			}
			if r.NUMA.State != want || r.NUMA.FractionSpread != nil {
				t.Fatalf("false distribution: %+v", r.NUMA)
			}
		})
	}
}

func TestNUMAThresholdBoundariesUseExactFractions(t *testing.T) {
	for _, tc := range []struct {
		name                         string
		total, firstFree, secondFree uint64
		want                         string
	}{
		{"exact twenty points", 1000 << 20, 400 << 20, 200 << 20, "within-threshold"},
		{"one KiB over", 1000 << 20, 400 << 20, (200 << 20) - 1024, "uneven-nonfree"},
		{"exact practical floor", 128 << 20, 128 << 20, 64 << 20, "within-threshold"},
		{"above practical floor", 128 << 20, 128 << 20, (64 << 20) - 1024, "uneven-nonfree"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := sample()
			o.NUMA.Items[0].TotalBytes = value(tc.total)
			o.NUMA.Items[1].TotalBytes = value(tc.total)
			o.NUMA.Items[0].FreeBytes = value(tc.firstFree)
			o.NUMA.Items[1].FreeBytes = value(tc.secondFree)
			r, err := Analyse(o, o.ReportedAt)
			if err != nil || r.NUMA.State != tc.want {
				t.Fatal(r.NUMA, err)
			}
		})
	}
}

func TestTopologyPreservesPartialPoolAndCounterEvidence(t *testing.T) {
	for _, scenario := range []string{"missing reservations", "free exceeds total", "overflow", "missing failures"} {
		t.Run(scenario, func(t *testing.T) {
			o := sample()
			o.Pools.Completeness = capability.Partial
			o.Pools.Reason = PartialFields
			switch scenario {
			case "missing reservations":
				o.Pools.Items[0].ReservedPages = nil
			case "free exceeds total":
				o.Pools.Items[0].FreePages = value(9)
			case "overflow":
				o.Pools.Items[0].TotalPages = value(math.MaxUint64)
			case "missing failures":
				o.Cgroup.Items[0].LimitFailures = nil
				o.Cgroup.Completeness = capability.Partial
				o.Cgroup.Reason = PartialFields
			}
			r, err := Analyse(o, o.ReportedAt)
			if err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "missing reservations":
				if r.Pools[0].ReservedBytes != nil || r.Pools[0].InUseBytes == nil {
					t.Fatal("missing reservation inferred")
				}
			case "free exceeds total", "overflow":
				if r.Pools[0].InUseBytes != nil || r.Pools[0].PoolBytes != nil {
					t.Fatal("invalid arithmetic appeared measured")
				}
			case "missing failures":
				if r.Observation.Cgroup.Items[0].LimitFailures != nil || r.CgroupState != "partial" {
					t.Fatal("unknown failure counter became zero")
				}
			}
		})
	}
}

func TestTopologyDeterminismAndSourceImmutability(t *testing.T) {
	o := sample()
	before, _ := json.Marshal(o)
	first, err := Analyse(o, o.ReportedAt)
	if err != nil {
		t.Fatal(err)
	}
	o.NUMA.Items[0], o.NUMA.Items[1] = o.NUMA.Items[1], o.NUMA.Items[0]
	second, err := Analyse(o, o.ReportedAt)
	if err != nil || !reflect.DeepEqual(first, second) {
		t.Fatal("input ordering changed analysis", err)
	}
	o.NUMA.Items[0], o.NUMA.Items[1] = o.NUMA.Items[1], o.NUMA.Items[0]
	after, _ := json.Marshal(o)
	if string(before) != string(after) {
		t.Fatal("input modified")
	}
}
