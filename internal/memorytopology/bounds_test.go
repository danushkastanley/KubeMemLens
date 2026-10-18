package memorytopology

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"
)

func TestTopologyRejectsMalformedScopeClockAndShape(t *testing.T) {
	for _, change := range []string{"empty UID", "control UID", "invalid name", "future clock", "false completeness", "duplicate node", "duplicate pool", "per-node reservation", "unaligned charge", "ambiguous limit", "duplicate cgroup node", "too many nodes"} {
		t.Run(change, func(t *testing.T) {
			o := sample()
			switch change {
			case "empty UID":
				o.NodeUID = ""
			case "control UID":
				o.NodeUID = "\x1b"
			case "invalid name":
				o.NodeName = "../other"
			case "future clock":
				o.ReportedAt = o.ReportedAt.Add(time.Hour)
			case "false completeness":
				o.NUMA.Items[0].FreeBytes = nil
			case "duplicate node":
				o.NUMA.Items[1].ID = 0
			case "duplicate pool":
				o.Pools.Items = append(o.Pools.Items, o.Pools.Items[0])
			case "per-node reservation":
				o.NUMA.Items[0].Pools = []HugePool{o.Pools.Items[0]}
			case "unaligned charge":
				o.Cgroup.Items[0].CurrentBytes = value(3)
			case "ambiguous limit":
				o.Cgroup.Items[0].Limit.Bytes = value(0)
			case "duplicate cgroup node":
				o.Cgroup.Items[0].NUMA[1].ID = 0
			case "too many nodes":
				o.NUMA.Items = make([]NUMANode, MaxNodes+1)
			}
			if o.Validate(sample().ReportedAt, 2*time.Minute) == nil {
				t.Fatal("invalid source accepted")
			}
		})
	}
}

func TestTopologyWireRejectsAmbiguityAndAlteredAnalysis(t *testing.T) {
	for _, body := range []string{`{"schemaVersion":1,"SchemaVersion":1}`, `{"unknown":0}`, `{"numa":{"items":[` + strings.Repeat(`{},`, MaxNodes) + `{}]}}`, `{"nodeName":"` + strings.Repeat("x", 513) + `"}`, `{} {}`} {
		var o Observation
		if json.Unmarshal([]byte(body), &o) == nil {
			t.Fatal("invalid wire accepted", body)
		}
	}
	o := sample()
	r, err := Analyse(o, o.ReportedAt)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var copy Report
	if json.Unmarshal(data, &copy) != nil || copy.Validate() != nil {
		t.Fatal("valid roundtrip rejected")
	}
	copy.NUMA.State = "within-threshold"
	if copy.Validate() == nil {
		t.Fatal("altered interpretation accepted")
	}
}

func TestMaximumCompleteTopologyProfileFitsBudget(t *testing.T) {
	o := sample()
	o.NodeName = strings.Repeat("n", 253)
	o.NodeUID = strings.Repeat("u", 128)
	o.NUMA.Items = nil
	o.Pools.Items = nil
	o.Cgroup.Items = nil
	for i := 0; i < MaxPageSizes; i++ {
		size := uint64(4096) << i
		pool := HugePool{PageSizeBytes: size, TotalPages: value(math.MaxUint64 / size), FreePages: value(0), ReservedPages: value(0), SurplusPages: value(0)}
		o.Pools.Items = append(o.Pools.Items, pool)
		domains := []DomainBytes{}
		for j := 0; j < MaxNodes; j++ {
			domains = append(domains, DomainBytes{ID: uint16(j + 65000), Bytes: 0})
		}
		o.Cgroup.Items = append(o.Cgroup.Items, HugeCgroup{PageSizeBytes: size, CurrentBytes: value(0), Limit: Limit{Unlimited: true}, ReservationBytes: value(0), ReservationLimit: Limit{Unlimited: true}, LimitFailures: value(math.MaxUint64), NUMATotalBytes: value(0), NUMA: domains})
	}
	for i := 0; i < MaxNodes; i++ {
		n := sample().NUMA.Items[0]
		n.ID = uint16(i + 65000)
		n.TotalBytes = value(math.MaxUint64)
		n.FreeBytes = value(0)
		n.Placement = &Placement{HitPages: value(math.MaxUint64), MissPages: value(math.MaxUint64), ForeignPages: value(math.MaxUint64), LocalPages: value(math.MaxUint64), OtherPages: value(math.MaxUint64), InterleavePages: value(math.MaxUint64)}
		for _, p := range o.Pools.Items {
			p.ReservedPages = nil
			n.Pools = append(n.Pools, p)
		}
		o.NUMA.Items = append(o.NUMA.Items, n)
	}
	encoded, err := json.Marshal(o)
	if err != nil || len(encoded) > MaxObservationBytes {
		t.Fatalf("maximum profile bytes=%d err=%v", len(encoded), err)
	}
	report, err := Analyse(o, o.ReportedAt)
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(report)
	if err != nil || len(body) > MaxReportBytes {
		t.Fatal(len(body), err)
	}
	t.Logf("maximum profile observation=%d report=%d bytes", len(encoded), len(body))
}
