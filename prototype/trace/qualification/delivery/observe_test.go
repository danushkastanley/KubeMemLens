package delivery

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	"github.com/danushkastanley/kube-memlens/internal/traceaggregate"
	"github.com/danushkastanley/kube-memlens/internal/traceframe"
)

func fixture(t *testing.T, path string) ([]byte, Expectation, time.Time, func() time.Time) {
	return fixtureAt(t, path, time.Unix(200, 0).UTC(), 30*time.Second)
}

func fixtureAt(t *testing.T, path string, start time.Time, duration time.Duration) ([]byte, Expectation, time.Time, func() time.Time) {
	t.Helper()
	end := start.Add(duration)
	target := trace.TargetIdentity{Namespace: "tenant", PodName: "target", PodUID: "private-pod", ContainerName: "worker", ContainerID: strings.Repeat("a", 64), ContainerStartedAt: start.Add(-100 * time.Second), NodeUID: "private-node", CgroupID: 12345}
	bounds := trace.DefaultBounds()
	bounds.Duration = duration
	spec, err := trace.NewSpecification(trace.Files, target, trace.ConfirmedPaths, bounds)
	if err != nil {
		t.Fatal(err)
	}
	expected := Expectation{strings.Repeat("b", 32), "sha256:" + strings.Repeat("c", 64), "sha256:" + strings.Repeat("d", 64), spec}
	meta, err := traceframe.NewMetadataVersion(traceframe.Metadata{SessionID: expected.SessionID, EngineDigest: expected.EngineDigest, ProgrammeDigest: expected.ProgrammeDigest, Specification: spec, SessionStartedAt: start, Deadline: end}, traceframe.AggregateVersion)
	if err != nil {
		t.Fatal(err)
	}
	encode := func(frame traceframe.Frame) []byte {
		b, err := traceframe.Encode(frame)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	data := encode(meta)
	amount, one, zero := uint64(65536), uint64(1), uint64(0)
	privatePath, err := trace.NewSensitiveText(path, spec.Bounds().PathBytes)
	if err != nil {
		t.Fatal(err)
	}
	activity := trace.FileActivity{ObservedAt: start.Add(duration / 2), Operation: trace.FileRead, RequestedBytes: &amount, CompletedBytes: &amount, Path: privatePath}
	frame, err := traceframe.NewFileVersion(activity, spec, traceframe.AggregateVersion)
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, encode(frame)...)
	aggregate, err := traceaggregate.New(trace.Files, 10000)
	if err != nil {
		t.Fatal(err)
	}
	if err := aggregate.File(activity); err != nil {
		t.Fatal(err)
	}
	summary := aggregate.Snapshot()
	uncertainty := time.Microsecond
	correlation := &trace.Correlation{State: "overlapping", EvidenceStart: start, BeforeEnd: start, AfterStart: end, EvidenceEnd: end, OverlapStart: start.Add(uncertainty), OverlapEnd: end.Add(-uncertainty), Uncertainty: &uncertainty, File: trace.GaugePair{Before: &zero, After: &zero}, Refault: trace.CounterDelta{State: "reported", Delta: &zero}, Scan: trace.CounterDelta{State: "reset"}, Steal: trace.CounterDelta{State: "unreported"}}
	last, err := traceframe.NewSummaryVersion(traceframe.Summary{SessionEndedAt: end, ObservationStartedAt: &start, ObservationEndedAt: &end, Termination: trace.Expired, EngineCounts: trace.Counts{Produced: &one, Sampled: &zero, Lost: &zero, Rejected: &zero}, WrittenEvents: 1, WrittenBytesBeforeSummary: uint64(len(data)), Incomplete: true, Aggregates: &summary, Correlation: correlation}, traceframe.AggregateVersion)
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, encode(last)...)
	times := []time.Time{start, start.Add(time.Millisecond), start.Add(duration/2 + 10*time.Millisecond), end.Add(time.Millisecond), end.Add(2 * time.Millisecond)}
	index := 0
	now := func() time.Time { value := times[index]; index++; return value }
	return data, expected, end, now
}

func TestProductionFramesProduceOnlyNumericTimingEvidence(t *testing.T) {
	data, expected, deadline, now := fixture(t, fixturePath)
	result, err := Observe(bytes.NewReader(data), expected, now, func() (time.Time, error) { return deadline, nil })
	if err != nil || !result.TransportComplete || !result.MetadataMatched || !result.HookCoverageIncomplete || len(result.Timings) != 1 {
		t.Fatal(result, err)
	}
	latency, err := Latencies(result)
	if err != nil || !latency.Passed || *latency.P99UpperNanos != int64(10*time.Millisecond+time.Microsecond) {
		t.Fatal(latency, err)
	}
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{fixturePath, "private-pod", "private-node", expected.SessionID, expected.EngineDigest, strings.Repeat("a", 64), "path", "target"} {
		if bytes.Contains(raw, []byte(secret)) {
			t.Fatal("private frame value exported")
		}
	}
}
func TestTargetAndPendingExpiryCannotReplaceActiveAdmission(t *testing.T) {
	for _, change := range []string{"session", "programme", "target", "pending-expiry"} {
		t.Run(change, func(t *testing.T) {
			data, expected, deadline, now := fixture(t, fixturePath)
			switch change {
			case "session":
				expected.SessionID = strings.Repeat("e", 32)
			case "programme":
				expected.ProgrammeDigest = "sha256:" + strings.Repeat("e", 64)
			case "target":
				target := expected.Specification.Target()
				target.PodUID = "replacement"
				spec, err := trace.NewSpecification(trace.Files, target, trace.ConfirmedPaths, trace.DefaultBounds())
				if err != nil {
					t.Fatal(err)
				}
				expected.Specification = spec
			case "pending-expiry":
				deadline = deadline.Add(-15 * time.Second)
			}
			result, err := Observe(bytes.NewReader(data), expected, now, func() (time.Time, error) { return deadline, nil })
			if err == nil || result.TransportComplete {
				t.Fatal("invalid admission qualified")
			}
		})
	}
}
func TestPrivatePathAndIncompleteTransportNeverQualify(t *testing.T) {
	for _, kind := range []string{"private-path", "missing-summary", "unknown-clock", "trailing-frame"} {
		t.Run(kind, func(t *testing.T) {
			path := fixturePath
			if kind == "private-path" {
				path = "/unapproved/private-file"
			}
			data, expected, deadline, now := fixture(t, path)
			switch kind {
			case "missing-summary":
				lines := bytes.Split(data, []byte("\n"))
				data = bytes.Join(lines[:2], []byte("\n"))
				data = append(data, '\n')
			case "unknown-clock":
				data = bytes.Replace(data, []byte(`"uncertaintyNanos":1000`), []byte(`"uncertaintyNanos":null`), 1)
			case "trailing-frame":
				data = append(data, []byte("{}\n")...)
			}

			result, err := Observe(bytes.NewReader(data), expected, now, func() (time.Time, error) { return deadline, nil })
			if err == nil || result.TransportComplete {
				t.Fatal("incomplete/private evidence qualified")
			}
			raw, _ := json.Marshal(result)
			if bytes.Contains(raw, []byte("private-file")) {
				t.Fatal("private path disclosed")
			}
		})
	}
}

func TestCompleteLossFailureIsRetainedAsCompleteTransport(t *testing.T) {
	data, expected, deadline, now := fixture(t, fixturePath)
	data = bytes.Replace(data, []byte(`"produced":1`), []byte(`"produced":2`), 1)
	data = bytes.Replace(data, []byte(`"lost":0`), []byte(`"lost":1`), 1)
	result, err := Observe(bytes.NewReader(data), expected, now, func() (time.Time, error) { return deadline, nil })
	if err != nil || !result.TransportComplete {
		t.Fatal("complete loss evidence discarded", result, err)
	}
	latency, err := Latencies(result)
	if err != nil || latency.Passed || latency.EventLossPercent != 50 || latency.P95UpperNanos != nil {
		t.Fatal(latency, err)
	}
}
