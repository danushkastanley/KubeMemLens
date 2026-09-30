package traceframe

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
)

type compatibilityGolden struct {
	data                []byte
	terminal            trace.Termination
	lost                *uint64
	incompleteTransport bool
	rejected            bool
}

func compatibilityGoldens(t *testing.T) map[string]compatibilityGolden {
	t.Helper()
	_, first, summary := aggregateFixture(t, trace.Files)
	firstBytes := encoded(t, first)
	join := func(first []byte, summary Summary, version int) []byte {
		t.Helper()
		last, err := NewSummaryVersion(summary, version)
		if err != nil {
			t.Fatal(err)
		}
		return append(bytes.Clone(first), encoded(t, last)...)
	}
	result := map[string]compatibilityGolden{
		"v2-file-expiry":       {data: join(firstBytes, summary, 2), terminal: trace.Expired},
		"v2-partial-transport": {data: bytes.Clone(firstBytes), incompleteTransport: true},
	}
	loss := summary
	produced, lost := uint64(2), uint64(1)
	loss.EngineCounts.Produced = &produced
	loss.EngineCounts.Lost = &lost
	result["v2-file-loss"] = compatibilityGolden{data: join(firstBytes, loss, 2), terminal: trace.Expired, lost: &lost}
	correlated := summary
	correlated.Correlation = frameCorrelation(*summary.ObservationStartedAt, *summary.ObservationEndedAt)
	result["v2-file-correlated"] = compatibilityGolden{data: join(firstBytes, correlated, 2), terminal: trace.Expired}
	m, _, truncated := aggregateFixture(t, trace.Files)
	b := m.Specification.Bounds()
	b.Events = 1
	var err error
	m.Specification, err = trace.NewSpecification(trace.Files, m.Specification.Target(), m.Specification.Paths(), b)
	if err != nil {
		t.Fatal(err)
	}
	limited, err := NewMetadataVersion(m, 2)
	if err != nil {
		t.Fatal(err)
	}
	limitedBytes := encoded(t, limited)
	truncated.WrittenBytesBeforeSummary = uint64(len(limitedBytes))
	truncated.Termination = trace.EventLimit
	result["v2-file-truncated"] = compatibilityGolden{data: join(limitedBytes, truncated, 2), terminal: trace.EventLimit}
	cancelled := Summary{SessionEndedAt: time.Unix(201, 0).UTC(), Termination: trace.Cancelled, Incomplete: true, WrittenBytesBeforeSummary: uint64(len(firstBytes))}
	result["v2-cancelled-unknown-counts"] = compatibilityGolden{data: join(firstBytes, cancelled, 2), terminal: trace.Cancelled}
	_, cacheFirst, cacheSummary := aggregateFixture(t, trace.Cache)
	result["v2-cache-expiry"] = compatibilityGolden{data: join(encoded(t, cacheFirst), cacheSummary, 2), terminal: trace.Expired}
	_, oomFirst, oomEvent, oomSummary := oomStreamFixture(t)
	oomPrefix := append(encoded(t, oomFirst), encoded(t, oomEvent)...)
	result["v3-oom-expiry"] = compatibilityGolden{data: join(oomPrefix, oomSummary, 3), terminal: trace.Expired}
	rich := richOOMGolden(oomSummary)
	result["v3-oom-correlated"] = compatibilityGolden{data: join(oomPrefix, rich, 3), terminal: trace.Expired}
	result["unknown-future-version"] = compatibilityGolden{data: bytes.Replace(firstBytes, []byte(`"version":2`), []byte(`"version":99`), 1), rejected: true}
	result["unknown-policy-field"] = compatibilityGolden{data: bytes.Replace(firstBytes, []byte(`"bounds":`), []byte(`"policyOverride":true,"bounds":`), 1), rejected: true}
	return result
}

func richOOMGolden(summary Summary) Summary {
	start, end := summary.ObservationStartedAt.Add(5*time.Millisecond), *summary.ObservationEndedAt
	summary.ObservationStartedAt = &start
	summary.SessionEndedAt = end.Add(3 * time.Millisecond)
	maximum, zero := ^uint64(0), uint64(0)
	delta := trace.CounterDelta{State: "reported", Delta: &maximum}
	deltas := trace.OOMEventDeltas{Low: delta, High: delta, Max: delta, OOM: delta, Kill: delta, GroupKill: delta}
	uncertainty := time.Microsecond
	summary.OOMCorrelation = &trace.OOMCorrelation{
		Window: trace.CorrelationWindow{State: "overlapping", EvidenceStart: start.Add(-time.Millisecond), BeforeEnd: start, AfterStart: end, EvidenceEnd: end.Add(time.Millisecond), OverlapStart: start.Add(uncertainty), OverlapEnd: end.Add(-uncertainty), Uncertainty: &uncertainty},
		Local:  deltas, Hierarchical: deltas, Current: trace.GaugePair{Before: &zero, After: &maximum},
		LimitBefore: trace.OOMLimit{State: "unlimited"}, LimitAfter: trace.OOMLimit{State: "finite", Bytes: &maximum}, PSISome: delta, PSIFull: delta,
	}
	summary.KubernetesContext = &trace.KubernetesOOMContext{State: "observed", BeforeStart: start.Add(-2 * time.Millisecond), BeforeEnd: start.Add(-time.Millisecond), AfterStart: end.Add(time.Millisecond), AfterEnd: end.Add(2 * time.Millisecond), Restarts: trace.CounterDelta{State: "reported", Delta: &zero}, PressureBefore: "false", PressureAfter: "unknown"}
	return summary
}

func TestVersionedGoldenEvidencePreservesMeaning(t *testing.T) {
	for name, fixture := range compatibilityGoldens(t) {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join("testdata", "compatibility", name+".ndjson")
			if os.Getenv("KML_WRITE_TRACE_GOLDENS") == "1" {
				if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, fixture.data, 0644); err != nil {
					t.Fatal(err)
				}
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(data, fixture.data) {
				t.Fatal("wire representation changed; review its version before updating the golden")
			}
			reader := NewReader(bytes.NewReader(data))
			last := Frame{}
			for {
				frame, err := reader.Next()
				if err == nil {
					last = frame
					continue
				}
				if fixture.rejected {
					if err == io.EOF || last.Type() != "" {
						t.Fatal("future contract was accepted")
					}
					return
				}
				if fixture.incompleteTransport {
					if err == io.EOF || last.Type() != MetadataFrame {
						t.Fatal("partial transport became a terminal result")
					}
					return
				}
				if err != io.EOF || last.Type() != SummaryFrame {
					t.Fatalf("known stream rejected: %v", err)
				}
				break
			}
			summary, err := last.ClientSummary()
			if err != nil || summary.Termination != fixture.terminal || !summary.Incomplete {
				t.Fatal("terminal evidence changed")
			}
			if fixture.lost != nil && (summary.EngineCounts.Lost == nil || *summary.EngineCounts.Lost != *fixture.lost) {
				t.Fatal("measured loss disappeared")
			}
			if fixture.terminal == trace.Cancelled && (summary.EngineCounts.Produced != nil || summary.ObservationStartedAt != nil) {
				t.Fatal("unknown cancelled evidence became a measurement")
			}
		})
	}
}
