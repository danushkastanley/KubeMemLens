package delivery

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	"github.com/danushkastanley/kube-memlens/internal/traceframe"
)

func ceilingFixture(t *testing.T, reason trace.Termination, limit uint64) ([]byte, Expectation, time.Time, func() time.Time) {
	t.Helper()
	return ceilingFixtureAt(t, reason, limit, time.Unix(200, 0).UTC(), 30*time.Second)
}

func ceilingFixtureAt(t *testing.T, reason trace.Termination, limit uint64, start time.Time, duration time.Duration) ([]byte, Expectation, time.Time, func() time.Time) {
	t.Helper()
	data, expected, deadline, now := fixtureAt(t, fixturePath, start, duration)
	reader := traceframe.NewReader(bytes.NewReader(data))
	first, err := reader.Next()
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := first.ClientMetadata()
	if err != nil {
		t.Fatal(err)
	}
	event, err := reader.Next()
	if err != nil {
		t.Fatal(err)
	}
	eventBytes, err := traceframe.Encode(event)
	if err != nil {
		t.Fatal(err)
	}
	last, err := reader.Next()
	if err != nil {
		t.Fatal(err)
	}
	summary, err := last.ClientSummary()
	if err != nil {
		t.Fatal(err)
	}
	bounds := expected.Specification.Bounds()
	bounds.Events = limit
	var metadataBytes []byte
	// Match a byte ceiling leaving one byte beyond the terminal reserve. Its
	// decimal representation settles before the final metadata encoding.
	for i := 0; i < 4; i++ {
		spec, err := trace.NewSpecification(trace.Files, expected.Specification.Target(), trace.ConfirmedPaths, bounds)
		if err != nil {
			t.Fatal(err)
		}
		expected.Specification = spec
		frame, err := traceframe.NewMetadataVersion(traceframe.Metadata{SessionID: expected.SessionID,
			EngineDigest: expected.EngineDigest, ProgrammeDigest: expected.ProgrammeDigest,
			Specification: spec, SessionStartedAt: metadata.SessionStartedAt, Deadline: deadline}, traceframe.AggregateVersion)
		if err != nil {
			t.Fatal(err)
		}
		metadataBytes, err = traceframe.Encode(frame)
		if err != nil {
			t.Fatal(err)
		}
		if reason == trace.OutputLimit {
			bounds.OutputBytes = uint64(len(metadataBytes)+len(eventBytes)+traceframe.AggregateTerminalReserve) + 1
		}
	}
	summary.Termination = reason
	summary.EngineCounts = trace.Counts{}
	summary.WrittenBytesBeforeSummary = uint64(len(metadataBytes) + len(eventBytes))
	terminal, err := traceframe.NewSummaryVersion(summary, traceframe.AggregateVersion)
	if err != nil {
		t.Fatal(err)
	}
	terminalBytes, err := traceframe.Encode(terminal)
	if err != nil {
		t.Fatal(err)
	}
	return append(append(metadataBytes, eventBytes...), terminalBytes...), expected, deadline, now
}

func TestCeilingObservationKeepsUnknownCountsAndPrivateValuesOutOfOutput(t *testing.T) {
	for _, reason := range []trace.Termination{trace.EventLimit, trace.OutputLimit} {
		t.Run(string(reason), func(t *testing.T) {
			limit := uint64(1)
			if reason == trace.OutputLimit {
				limit = 10000
			}
			data, expected, deadline, now := ceilingFixture(t, reason, limit)
			result, err := ObserveCeiling(bytes.NewReader(data), expected, now, func() (time.Time, error) { return deadline, nil })
			if err != nil || !result.TransportComplete || !result.CeilingReported || result.Events != 1 || result.Counts == nil || result.Counts.Produced != nil || result.Counts.Lost != nil {
				t.Fatal(result, err)
			}
			if reason == trace.OutputLimit && result.EncodedBytes >= expected.Specification.Bounds().OutputBytes {
				t.Fatal("output ceiling incorrectly requires every byte to be filled")
			}
			raw, _ := json.Marshal(result)
			for _, secret := range []string{fixturePath, "private-pod", "private-node", expected.SessionID, expected.EngineDigest} {
				if bytes.Contains(raw, []byte(secret)) {
					t.Fatal("private stream value exported")
				}
			}
			if !bytes.Contains(raw, []byte(`"produced":null`)) || bytes.Contains(raw, []byte("p99")) {
				t.Fatal("unknown loss or latency was manufactured")
			}
		})
	}
}

func TestOutputLimitWithRoomForEveryPossibleNextFrameIsNotProven(t *testing.T) {
	data, expected, deadline, now := ceilingFixture(t, trace.EventLimit, 10000)
	lines := bytes.Split(bytes.TrimSpace(data), []byte("\n"))
	lines[2] = bytes.Replace(lines[2], []byte(`"event_limit"`), []byte(`"output_limit"`), 1)
	data = append(bytes.Join(lines, []byte("\n")), '\n')
	result, err := ObserveCeiling(bytes.NewReader(data), expected, now, func() (time.Time, error) { return deadline, nil })
	if err == nil || !result.TransportComplete || result.CeilingReported {
		t.Fatal("early output-limit label treated as ceiling evidence", result, err)
	}
}

func TestExpiredOrPrematureEventLimitDoesNotProveACeiling(t *testing.T) {
	for _, item := range []struct {
		reason trace.Termination
		limit  uint64
	}{{trace.Expired, 1}, {trace.EventLimit, 2}, {trace.Cancelled, 1}} {
		data, expected, deadline, now := ceilingFixture(t, item.reason, item.limit)
		result, err := ObserveCeiling(bytes.NewReader(data), expected, now, func() (time.Time, error) { return deadline, nil })
		if err == nil || !result.TransportComplete || result.CeilingReported {
			t.Fatal("non-ceiling transport qualified", result, err)
		}
	}
}

func TestCeilingObservationPreservesNormalExpiryOnlyContract(t *testing.T) {
	data, expected, deadline, now := ceilingFixture(t, trace.EventLimit, 1)
	result, err := Observe(bytes.NewReader(data), expected, now, func() (time.Time, error) { return deadline, nil })
	if err == nil || result.TransportComplete {
		t.Fatal("normal receiver accepted a flood terminal")
	}
}

func TestCeilingObservationRejectsWrongLifetimeAndBrokenTransport(t *testing.T) {
	for _, change := range []string{"deadline", "session", "missing-summary", "extra-frame", "path"} {
		data, expected, deadline, now := ceilingFixture(t, trace.EventLimit, 1)
		switch change {
		case "deadline":
			deadline = deadline.Add(-time.Second)
		case "session":
			expected.SessionID = "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
		case "missing-summary":
			lines := bytes.SplitAfter(data, []byte("\n"))
			data = append(lines[0], lines[1]...)
		case "extra-frame":
			data = append(data, []byte("{}\n")...)
		case "path":
			data, expected, deadline, now = fixture(t, "/work/unowned.bin")
		}
		result, err := ObserveCeiling(bytes.NewReader(data), expected, now, func() (time.Time, error) { return deadline, nil })
		if err == nil || result.TransportComplete {
			t.Fatal("invalid ceiling stream accepted", change, result, err)
		}
	}
}
