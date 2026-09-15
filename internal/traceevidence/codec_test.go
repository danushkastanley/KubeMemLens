package traceevidence

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
)

func evidence() (*trace.Correlation, time.Time, time.Time) {
	start, end := time.Unix(20, 0).UTC(), time.Unix(22, 0).UTC()
	uncertainty, zero, one := time.Microsecond, uint64(0), uint64(1)
	return &trace.Correlation{State: "overlapping", EvidenceStart: start.Add(-time.Millisecond), BeforeEnd: start, AfterStart: end, EvidenceEnd: end.Add(time.Millisecond), OverlapStart: start.Add(uncertainty), OverlapEnd: end.Add(-uncertainty), Uncertainty: &uncertainty,
		File: trace.GaugePair{Before: &one, After: &zero}, Dirty: trace.GaugePair{Before: &zero, After: &zero},
		Refault: trace.CounterDelta{State: "reported", Delta: &zero}, Scan: trace.CounterDelta{State: "reset"}, Steal: trace.CounterDelta{State: "unreported"}}, start, end
}

func TestCorrelationCodecPreservesZeroMissingResetAndOwnedValues(t *testing.T) {
	c, start, end := evidence()
	data, err := Encode(c, start, end, 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := Decode(data, start, end, 3*time.Second)
	if err != nil || *decoded.File.After != 0 || decoded.Writeback.After != nil || decoded.Scan.State != "reset" || decoded.Steal.State != "unreported" || *decoded.Uncertainty != time.Microsecond {
		t.Fatal("evidence distinctions lost")
	}
	*c.File.Before = 55
	if *decoded.File.Before != 1 {
		t.Fatal("decoded evidence aliases producer")
	}
	if _, err := json.Marshal(decoded); err == nil {
		t.Fatal("ordinary JSON disclosed evidence")
	}
	if fmt.Sprint(decoded) != "[ephemeral cgroup correlation]" {
		t.Fatal("ordinary formatting disclosed evidence")
	}
}

func TestCorrelationRejectsAmbiguousAndUnboundedEvidence(t *testing.T) {
	c, start, end := evidence()
	data, err := Encode(c, start, end, 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range [][]byte{
		bytes.Replace(data, []byte(`"state":"overlapping"`), []byte(`"state":"unavailable"`), 1),
		bytes.Replace(data, []byte(`"uncertaintyNanos":1000`), []byte(`"uncertaintyNanos":5000001`), 1),
		bytes.Replace(data, []byte(`"state":"reset","delta":null`), []byte(`"state":"reset","delta":0`), 1),
		bytes.Replace(data, []byte(`"after":0`), []byte(`"after":0,"after":1`), 1),
		bytes.Replace(data, []byte(`"before":1`), []byte(`"Before":1`), 1),
		bytes.Replace(data, []byte(`"after":0`), []byte(`"after":18446744073709551616`), 1),
		[]byte(`{"state":"unavailable","fileBytes":{"before":0,"after":0}}`),
	} {
		if _, err := Decode(bad, start, end, 3*time.Second); err == nil {
			t.Fatal("ambiguous evidence accepted")
		}
	}
	if _, err := Decode(data, start, end, time.Second); err == nil {
		t.Fatal("window exceeded admitted duration")
	}
	c.OverlapEnd = c.OverlapEnd.Add(time.Microsecond)
	if _, err := Encode(c, start, end, 3*time.Second); err == nil {
		t.Fatal("uncertainty was removed from guaranteed overlap")
	}
	for _, state := range []string{"unavailable", "disjoint", "target_changed", "clock_uncertain"} {
		data, err := Encode(&trace.Correlation{State: state}, time.Time{}, time.Time{}, time.Second)
		if err != nil || strings.Contains(string(data), "Bytes") {
			t.Fatal("unavailable evidence contained data")
		}
		if _, err := Decode(data, time.Time{}, time.Time{}, time.Second); err != nil {
			t.Fatal(err)
		}
	}
}

func FuzzDecode(f *testing.F) {
	c, start, end := evidence()
	data, err := Encode(c, start, end, 3*time.Second)
	if err != nil {
		f.Fatal(err)
	}
	f.Add([]byte(data))
	f.Add([]byte(`{"state":"unavailable"}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		c, err := Decode(data, start, end, 3*time.Second)
		if err != nil {
			return
		}
		encoded, err := Encode(c, start, end, 3*time.Second)
		if err != nil || !bytes.Equal(data, encoded) {
			t.Fatal("accepted correlation changed representation")
		}
	})
}
