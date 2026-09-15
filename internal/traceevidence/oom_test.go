package traceevidence

import (
	"bytes"
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
)

func oomEvidenceFixture() (*trace.OOMCorrelation, time.Time, time.Time) {
	base := time.Unix(100, 0).UTC()
	start, end := base.Add(time.Second), base.Add(2*time.Second)
	uncertainty := time.Microsecond
	zero, maximum := uint64(0), uint64(math.MaxUint64)
	known := trace.CounterDelta{State: "reported", Delta: &maximum}
	unreported := trace.CounterDelta{State: "unreported"}
	deltas := trace.OOMEventDeltas{Low: known, High: known, Max: known, OOM: known, Kill: known, GroupKill: known}
	return &trace.OOMCorrelation{
		Window: trace.CorrelationWindow{State: "overlapping", EvidenceStart: base, BeforeEnd: base.Add(time.Millisecond), AfterStart: base.Add(3 * time.Second), EvidenceEnd: base.Add(3*time.Second + time.Millisecond), OverlapStart: start.Add(uncertainty), OverlapEnd: end.Add(-uncertainty), Uncertainty: &uncertainty},
		Local:  deltas, Hierarchical: deltas, Current: trace.GaugePair{Before: &zero, After: &maximum},
		LimitBefore: trace.OOMLimit{State: "unlimited"}, LimitAfter: trace.OOMLimit{State: "finite", Bytes: &maximum},
		PSISome: known, PSIFull: unreported,
	}, start, end
}

func TestOOMEvidenceRoundTripAndMaximumNumbers(t *testing.T) {
	value, start, end := oomEvidenceFixture()
	data, err := OOMEncode(value, start, end, 30*time.Second)
	if err != nil || len(data) > MaxOOMBytes {
		t.Fatal("bounded OOM evidence cannot encode")
	}
	got, err := OOMDecode(data, start, end, 30*time.Second)
	if err != nil || got.Local.Kill.Delta == nil || *got.Local.Kill.Delta != math.MaxUint64 || got.Current.Before == nil || *got.Current.Before != 0 || got.PSIFull.State != "unreported" {
		t.Fatal("OOM evidence lost value or missing state")
	}
	if got.LimitBefore.State != "unlimited" || got.LimitBefore.Bytes != nil || got.LimitAfter.State != "finite" {
		t.Fatal("limit semantics changed")
	}
	if _, err := json.Marshal(value); err == nil {
		t.Fatal("OOM evidence entered ordinary JSON")
	}
}

func TestOOMEvidenceRejectsAmbiguousOrOutOfWindowData(t *testing.T) {
	value, start, end := oomEvidenceFixture()
	data, _ := OOMEncode(value, start, end, 30*time.Second)
	for _, invalid := range [][]byte{
		bytes.Replace(data, []byte(`"state":"overlapping"`), []byte(`"state":"overlapping","state":"overlapping"`), 1),
		bytes.Replace(data, []byte(`"state"`), []byte(`"State"`), 1),
		bytes.Replace(data, []byte(`"local":`), []byte(`"unknown":`), 1),
		bytes.Replace(data, []byte(`"unlimited"`), []byte(`"finite"`), 1),
		[]byte(`{"state":"unavailable","currentBytes":{"before":0,"after":0}}`),
		append(data, ' '),
		make([]byte, MaxOOMBytes+1),
	} {
		if _, err := OOMDecode(invalid, start, end, 30*time.Second); err == nil {
			t.Fatal("invalid OOM evidence accepted")
		}
	}
	value.Window.OverlapEnd = end
	if _, err := OOMEncode(value, start, end, 30*time.Second); err == nil {
		t.Fatal("uncertainty-free overlap was invented")
	}
}

func TestUnavailableOOMEvidenceContainsNoMeasurements(t *testing.T) {
	for _, state := range []string{"unavailable", "target_changed", "clock_uncertain", "disjoint"} {
		value := &trace.OOMCorrelation{Window: trace.CorrelationWindow{State: state}}
		data, err := OOMEncode(value, time.Time{}, time.Time{}, time.Second)
		if err != nil || string(data) != `{"state":"`+state+`"}` {
			t.Fatal("unavailable evidence fabricated fields")
		}
		zero := uint64(0)
		value.Current.Before = &zero
		if _, err := OOMEncode(value, time.Time{}, time.Time{}, time.Second); err == nil {
			t.Fatal("unavailable state retained a measurement")
		}
	}
}
