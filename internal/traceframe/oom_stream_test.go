package traceframe

import (
	"bytes"
	"io"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	"github.com/danushkastanley/kube-memlens/internal/traceaggregate"
)

func oomStreamFixture(t *testing.T) (Metadata, Frame, Frame, Summary) {
	t.Helper()
	s := spec(t, trace.OOM, trace.OmitPaths)
	start, end := time.Unix(200, 0).UTC(), time.Unix(230, 0).UTC()
	m := Metadata{strings.Repeat("b", 32), "sha256:" + strings.Repeat("c", 64), "sha256:" + strings.Repeat("d", 64), s, start, end}
	first, err := NewMetadataVersion(m, OOMVersion)
	if err != nil {
		t.Fatal(err)
	}
	pid := uint32(1234)
	command, _ := trace.NewSensitiveText("fixture", 16)
	event := trace.OOMDecision{ObservedAt: start.Add(time.Second), Scope: trace.OOMScopeCgroup, VictimPID: &pid, Command: command}
	middle, err := NewOOMVersion(event, s, OOMVersion)
	if err != nil {
		t.Fatal(err)
	}
	acc, _ := traceaggregate.New(trace.OOM, 10)
	if acc.OOM(event) != nil {
		t.Fatal("OOM fixture aggregate failed")
	}
	aggregate := acc.Snapshot()
	one, zero := uint64(1), uint64(0)
	return m, first, middle, Summary{SessionEndedAt: end, ObservationStartedAt: &start, ObservationEndedAt: &end, Termination: trace.Expired, EngineCounts: trace.Counts{Produced: &one, Sampled: &zero, Lost: &zero, Rejected: &zero}, WrittenEvents: 1, WrittenBytesBeforeSummary: uint64(len(encoded(t, first)) + len(encoded(t, middle))), Incomplete: true, Aggregates: &aggregate, OOMCorrelation: &trace.OOMCorrelation{Window: trace.CorrelationWindow{State: "unavailable"}}}
}

func TestOOMRichSummaryFitsReservedFrameAndPreservesEvidence(t *testing.T) {
	_, first, middle, summary := oomStreamFixture(t)
	start, end := *summary.ObservationStartedAt, *summary.ObservationEndedAt
	start = start.Add(5 * time.Millisecond)
	summary.ObservationStartedAt = &start
	maximum, zero := uint64(math.MaxUint64), uint64(0)
	delta := trace.CounterDelta{State: "reported", Delta: &maximum}
	deltas := trace.OOMEventDeltas{Low: delta, High: delta, Max: delta, OOM: delta, Kill: delta, GroupKill: delta}
	uncertainty := time.Microsecond
	summary.SessionEndedAt = end.Add(time.Millisecond)
	summary.OOMCorrelation = &trace.OOMCorrelation{
		Window: trace.CorrelationWindow{State: "overlapping", EvidenceStart: start.Add(-time.Millisecond), BeforeEnd: start, AfterStart: end, EvidenceEnd: end.Add(time.Millisecond), OverlapStart: start.Add(uncertainty), OverlapEnd: end.Add(-uncertainty), Uncertainty: &uncertainty},
		Local:  deltas, Hierarchical: deltas, Current: trace.GaugePair{Before: &zero, After: &maximum},
		LimitBefore: trace.OOMLimit{State: "unlimited"}, LimitAfter: trace.OOMLimit{State: "finite", Bytes: &maximum}, PSISome: delta, PSIFull: delta,
	}
	last, err := NewSummaryVersion(summary, OOMVersion)
	if err != nil {
		t.Fatal(err)
	}
	kubernetes := &trace.KubernetesOOMContext{State: "observed", BeforeStart: start.Add(-2 * time.Millisecond), BeforeEnd: start.Add(-time.Millisecond), AfterStart: end.Add(time.Millisecond), AfterEnd: end.Add(2 * time.Millisecond), Restarts: trace.CounterDelta{State: "reported", Delta: &zero}, PressureBefore: "false", PressureAfter: "unknown"}
	last, err = last.WithOOMKubernetesContext(kubernetes, end.Add(3*time.Millisecond), 30*time.Second)
	if err != nil || len(encoded(t, last)) > int(OOMTerminalReserve) {
		t.Fatal("rich summary exceeded its reserved frame", err)
	}
	r := NewReader(bytes.NewReader(append(append(encoded(t, first), encoded(t, middle)...), encoded(t, last)...)))
	for range 3 {
		if _, err := r.Next(); err != nil {
			t.Fatal("rich OOM evidence rejected by public reader", err)
		}
	}
	if _, err := r.Next(); err != io.EOF {
		t.Fatal("rich OOM stream did not end")
	}
	data := encoded(t, last)
	for _, expected := range []string{`"oomKills":{"state":"reported","delta":18446744073709551615}`, `"pressureAfter":"unknown"`} {
		if !bytes.Contains(data, []byte(expected)) {
			t.Fatal("rich OOM summary lost a measured or unknown value")
		}
	}
}

func TestOOMVersionRoundTripAndKindIsolation(t *testing.T) {
	m, first, middle, summary := oomStreamFixture(t)
	last, err := NewSummaryVersion(summary, OOMVersion)
	if err != nil {
		t.Fatal(err)
	}
	stream := append(append(encoded(t, first), encoded(t, middle)...), encoded(t, last)...)
	r := NewReader(bytes.NewReader(stream))
	for range 3 {
		frame, err := r.Next()
		if err != nil || frame.Version() != OOMVersion {
			t.Fatal("valid OOM stream rejected")
		}
	}
	if _, err := r.Next(); err != io.EOF {
		t.Fatal("OOM stream did not end")
	}
	if _, err := NewMetadataVersion(m, AggregateVersion); err == nil {
		t.Fatal("OOM entered file/cache version")
	}
	if _, err := NewSummaryVersion(summary, Version); err == nil {
		t.Fatal("OOM evidence changed legacy version")
	}
	m.Specification = spec(t, trace.Files, trace.OmitPaths)
	if _, err := NewMetadataVersion(m, OOMVersion); err == nil {
		t.Fatal("file trace entered OOM version")
	}
}

func TestOOMReaderChecksScopeCountsAgainstDeliveredEvents(t *testing.T) {
	_, first, middle, summary := oomStreamFixture(t)
	summary.Aggregates.OOM.Cgroup = 0
	summary.Aggregates.OOM.Global = 1
	last, err := NewSummaryVersion(summary, OOMVersion)
	if err != nil {
		t.Fatal(err)
	}
	r := NewReader(bytes.NewReader(append(append(encoded(t, first), encoded(t, middle)...), encoded(t, last)...)))
	if _, err := r.Next(); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Next(); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Next(); err == nil {
		t.Fatal("summary rewrote a cgroup event as global scope")
	}
}

func TestOOMVersionRejectsInvalidProcessContextAndAuthorityLeak(t *testing.T) {
	m, _, _, summary := oomStreamFixture(t)
	command, _ := trace.NewSensitiveText("x\x00hidden", 16)
	event := trace.OOMDecision{ObservedAt: m.SessionStartedAt, Scope: trace.OOMScopeCgroup, Command: command}
	legacy, err := NewOOM(event, m.Specification)
	if err != nil {
		t.Fatal("legacy escaped text behaviour changed")
	}
	if _, err := NewOOMVersion(event, m.Specification, OOMVersion); err == nil {
		t.Fatal("new version accepted hidden process context")
	}
	data := bytes.Replace(encoded(t, legacy), []byte(`"version":1`), []byte(`"version":3`), 1)
	if _, err := Decode(data); err == nil {
		t.Fatal("decoder accepted invalid OOM context")
	}
	summary.Termination = trace.AuthorisationLost
	if _, err := NewSummaryVersion(summary, OOMVersion); err == nil {
		t.Fatal("authority loss retained rich OOM evidence")
	}
}
