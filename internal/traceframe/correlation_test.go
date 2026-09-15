package traceframe

import (
	"bytes"
	"io"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
)

func frameCorrelation(start, end time.Time) *trace.Correlation {
	uncertainty, zero := time.Microsecond, uint64(0)
	return &trace.Correlation{State: "overlapping", EvidenceStart: start, BeforeEnd: start, AfterStart: end, EvidenceEnd: end, OverlapStart: start.Add(uncertainty), OverlapEnd: end.Add(-uncertainty), Uncertainty: &uncertainty, File: trace.GaugePair{Before: &zero, After: &zero}, Refault: trace.CounterDelta{State: "reported", Delta: &zero}, Scan: trace.CounterDelta{State: "reset"}, Steal: trace.CounterDelta{State: "unreported"}}
}

func TestV2CorrelationRoundTripAndSessionBounds(t *testing.T) {
	metadata, first, last := aggregateFixture(t, trace.Files)
	last.Correlation = frameCorrelation(*last.ObservationStartedAt, *last.ObservationEndedAt)
	frame, err := NewSummaryVersion(last, AggregateVersion)
	if err != nil {
		t.Fatal(err)
	}
	data := encoded(t, frame)
	if _, err := Decode(data); err != nil {
		t.Fatal(err)
	}
	reader := NewReader(bytes.NewReader(append(encoded(t, first), data...)))
	for range 2 {
		if _, err := reader.Next(); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := reader.Next(); err != io.EOF {
		t.Fatal("unexpected output")
	}
	if _, err := NewSummary(last); err == nil {
		t.Fatal("v1 silently discarded correlation")
	}
	last.Correlation.EvidenceStart = metadata.SessionStartedAt.Add(-time.Millisecond)
	frame, err = NewSummaryVersion(last, AggregateVersion)
	if err != nil {
		t.Fatal(err)
	}
	reader = NewReader(bytes.NewReader(append(encoded(t, first), encoded(t, frame)...)))
	if _, err := reader.Next(); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.Next(); err == nil {
		t.Fatal("another session's evidence accepted")
	}
}

func TestV2CorrelationRequiresAuthorityAndKnownWindow(t *testing.T) {
	_, _, last := aggregateFixture(t, trace.Cache)
	last.Correlation = frameCorrelation(*last.ObservationStartedAt, *last.ObservationEndedAt)
	last.ObservationStartedAt, last.ObservationEndedAt = nil, nil
	if _, err := NewSummaryVersion(last, AggregateVersion); err == nil {
		t.Fatal("rich evidence accepted without observation window")
	}
	last.Correlation = &trace.Correlation{State: "clock_uncertain"}
	frame, err := NewSummaryVersion(last, AggregateVersion)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Decode(encoded(t, frame)); err != nil {
		t.Fatal(err)
	}
	last.Termination = trace.AuthorisationLost
	last.Aggregates = nil
	last.EngineCounts = trace.Counts{}
	if _, err := NewSummaryVersion(last, AggregateVersion); err == nil {
		t.Fatal("authority loss carried correlation")
	}
}
