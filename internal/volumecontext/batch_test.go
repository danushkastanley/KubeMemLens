package volumecontext

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/volumehealth"
)

func batchFixture(t *testing.T) Batch {
	t.Helper()
	s, _, u := fixture()
	b, err := NewBatch(s.NodeName, s.NodeUID, testNow, SourceState("reported", ""), u, testNow)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestPrivateBatchRoundTripAndIsolation(t *testing.T) {
	b := batchFixture(t)
	data, err := b.EncodePrivate()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodePrivate(data, testNow)
	if err != nil {
		t.Fatal(err)
	}
	s, _, _ := fixture()
	if !reflect.DeepEqual(decoded.ForPod(s), b.ForPod(s)) {
		t.Fatal("batch round trip changed observations")
	}
	if n, u := decoded.NodeIdentity(); n != s.NodeName || u != s.NodeUID || decoded.ReportedAt() != testNow || decoded.Len() != 1 || decoded.State().Availability != "reported" {
		t.Fatal("batch metadata changed")
	}
	got := decoded.ForPod(s)
	*got[0].Filesystem.UsedBytes = 42
	if *decoded.ForPod(s)[0].Filesystem.UsedBytes != 0 {
		t.Fatal("returned data is mutable shared state")
	}
	for _, mutate := range []func(*PodScope){func(s *PodScope) { s.Namespace = "tenant-b" }, func(s *PodScope) { s.PodUID = "recreated" }, func(s *PodScope) { s.NodeUID = "replaced" }, func(s *PodScope) { s.NodeName = "other" }} {
		copy := s
		mutate(&copy)
		if len(decoded.ForPod(copy)) != 0 {
			t.Fatal("batch selection crossed scope")
		}
	}
	public, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	if string(public) != `{"records":1,"availability":"reported"}` {
		t.Fatalf("default export changed: %s", public)
	}
	for _, v := range []any{b, s, decoded.ForPod(s)[0]} {
		text := fmt.Sprintf("%v %+v %#v", v, v, v)
		if strings.Contains(text, "tenant-a") || strings.Contains(text, "pod-uid") {
			t.Fatal("formatted private identity")
		}
	}
}

func TestPrivateBatchRejectsHostileWire(t *testing.T) {
	b := batchFixture(t)
	data, err := b.EncodePrivate()
	if err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string][]byte{
		"trailing JSON":       append(append([]byte{}, data...), []byte(` {}`)...),
		"duplicate":           bytes.Replace(data, []byte(`"schemaVersion":1`), []byte(`"schemaVersion":1,"schemaVersion":1`), 1),
		"alias":               bytes.Replace(data, []byte(`"nodeUID"`), []byte(`"nodeuID"`), 1),
		"unknown field":       bytes.Replace(data, []byte(`"schemaVersion":1`), []byte(`"schemaVersion":1,"secret":"value"`), 1),
		"future schema":       bytes.Replace(data, []byte(`"schemaVersion":1`), []byte(`"schemaVersion":2`), 1),
		"unknown enum":        bytes.Replace(data, []byte(`"reported"`), []byte(`"surprise"`), 1),
		"negative bytes":      bytes.Replace(data, []byte(`"usedBytes":0`), []byte(`"usedBytes":-1`), 1),
		"overflow bytes":      bytes.Replace(data, []byte(`"usedBytes":0`), []byte(`"usedBytes":18446744073709551616`), 1),
		"fraction bytes":      bytes.Replace(data, []byte(`"usedBytes":0`), []byte(`"usedBytes":0.1`), 1),
		"deep":                []byte(`{"records":[[[[[[[[[[[0]]]]]]]]]]]}`),
		"oversized":           bytes.Repeat([]byte(" "), MaxBatchBytes+1),
		"too many rows":       []byte(`{"records":[` + strings.Repeat(`{},`, MaxBatchRecords) + `{}]}`),
		"terminal control":    bytes.Replace(data, []byte(`"node-a"`), []byte(`"node\u001b-a"`), 1),
		"bidi":                bytes.Replace(data, []byte(`"node-a"`), []byte(`"node\u202e-a"`), 1),
		"missing measurement": bytes.Replace(data, []byte(`"capacityBytes":100,"usedBytes":0,"availableBytes":90,"inodes":1000,"inodesUsed":5`), nil, 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodePrivate(body, testNow); err == nil {
				t.Fatal("hostile wire accepted")
			}
		})
	}
	if _, err := DecodePrivate(data, testNow.Add(ExpireAfter+time.Second)); err == nil {
		t.Fatal("expired producer batch accepted")
	}
}

func TestBatchAndPodBounds(t *testing.T) {
	s, b, u := fixture()
	for count := 0; count < MaxVolumesPerPod; count++ {
		copy := u[0]
		copy.VolumeName = fmt.Sprintf("volume-%d", count)
		if count == 0 {
			u = nil
		}
		u = append(u, copy)
	}
	if _, err := NewBatch(s.NodeName, s.NodeUID, testNow, SourceState("reported", ""), u, testNow); err != nil {
		t.Fatal(err)
	}
	copy := u[0]
	copy.VolumeName = "one-too-many"
	u = append(u, copy)
	if _, err := NewBatch(s.NodeName, s.NodeUID, testNow, SourceState("reported", ""), u, testNow); err == nil {
		t.Fatal("unbounded Pod volumes")
	}
	if _, err := NewBatch(s.NodeName, s.NodeUID, testNow, SourceState("reported", ""), make([]RawUsage, MaxBatchRecords+1), testNow); err == nil {
		t.Fatal("unbounded batch")
	}
	for _, mutate := range []func(*Binding){
		func(b *Binding) { b.VolumeName = strings.Repeat("a", 64) }, func(b *Binding) { b.PVCUID = strings.Repeat("u", MaxUIDBytes+1) },
		func(b *Binding) { b.Configuration.MountCount = MaxMountsPerVolume + 1 }, func(b *Binding) { b.Configuration.ReadOnlyMountCount = 2 },
		func(b *Binding) { b.Configuration.Kind = "unknown" }, func(b *Binding) { b.Configuration.MemoryBacked = true },
	} {
		copy := b[0]
		mutate(&copy)
		if _, err := Join(s, []Binding{copy}, nil, nil, SourceState("disabled", Disabled), testNow); err == nil {
			t.Fatal("invalid configuration accepted")
		}
	}
}

func TestHealthTextAndConditionBounds(t *testing.T) {
	s, b, _ := fixture()
	for _, mutate := range []func(*volumeHealthTestInput){
		func(x *volumeHealthTestInput) { x.status = strings.Repeat("x", MaxStatusBytes+1) },
		func(x *volumeHealthTestInput) { x.reason = strings.Repeat("x", MaxReasonBytes+1) },
		func(x *volumeHealthTestInput) { x.message = strings.Repeat("x", 1025) },
		func(x *volumeHealthTestInput) { x.reason = "\x1b[31mhidden" },
		func(x *volumeHealthTestInput) { x.reason = "\u202ehidden" },
	} {
		input := volumeHealthTestInput{status: "Degraded"}
		mutate(&input)
		h := healthFixture("pod-node-plugin", input.status)
		h.Conditions[0].Reason = input.reason
		h.Conditions[0].Message = input.message
		if _, err := Join(s, b, nil, []HealthObservation{h}, SourceState("unreported", NoReport), testNow); err == nil {
			t.Fatal("unbounded or unsafe health text accepted")
		}
	}
	h := healthFixture("pod-node-plugin", "Degraded")
	for len(h.Conditions) <= volumehealth.MaxConditions {
		h.Conditions = append(h.Conditions, h.Conditions[0])
	}
	if _, err := Join(s, b, nil, []HealthObservation{h}, SourceState("unreported", NoReport), testNow); err == nil {
		t.Fatal("unbounded conditions")
	}
}

type volumeHealthTestInput struct{ status, reason, message string }

func FuzzPrivateBatch(f *testing.F) {
	f.Add([]byte(`{"schemaVersion":1,"nodeName":"node-a","nodeUID":"uid","reportedAt":"2026-09-12T12:00:00Z","state":{"source":"kubelet-summary","availability":"unreported","reason":"no-report","freshness":"unknown","completeness":"partial"},"records":[]}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		b, err := DecodePrivate(data, testNow)
		if err != nil {
			return
		}
		encoded, err := b.EncodePrivate()
		if err != nil {
			t.Fatal(err)
		}
		if len(encoded) > MaxBatchBytes || b.Len() > MaxBatchRecords {
			t.Fatal("accepted unbounded batch")
		}
		if _, err := DecodePrivate(encoded, testNow); err != nil {
			t.Fatal("accepted batch did not round trip")
		}
	})
}
