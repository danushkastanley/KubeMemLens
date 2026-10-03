package delivery

import (
	"encoding/json"
	"testing"
	"time"
)

func TestClockDiagnosticRetainsOnlyElapsedNumericReadings(t *testing.T) {
	start := time.Unix(1800000000, 0)
	rejected := start.Add(7*time.Second + 6*time.Millisecond)
	followUp := start.Add(7*time.Second + 7*time.Millisecond)
	value := clockDiagnostic(start, rejected, followUp)
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]int64
	if err := json.Unmarshal(raw, &fields); err != nil || len(fields) != 4 {
		t.Fatal("diagnostic is not restricted to four numeric readings", err)
	}
	for _, field := range []string{"rejectedWallElapsedNanos", "rejectedMonotonicElapsedNanos"} {
		if fields[field] != int64(7*time.Second+6*time.Millisecond) {
			t.Fatal("rejected elapsed time changed")
		}
	}
	for _, field := range []string{"followUpWallElapsedNanos", "followUpMonotonicElapsedNanos"} {
		if fields[field] != int64(7*time.Second+7*time.Millisecond) {
			t.Fatal("follow-up elapsed time changed")
		}
	}
}

func TestClockDiagnosticCannotMakeRejectedTransportQualify(t *testing.T) {
	for _, nextOffset := range []time.Duration{0, 6 * time.Millisecond} {
		result := timingResult(10 * time.Millisecond)
		result.TransportComplete = false
		result.ClientClockDriftNanos = int64(6 * time.Millisecond)
		result.ClockDiagnostic = &ClockDiagnostic{
			RejectedWallElapsedNanos: int64(time.Second), RejectedMonotonicElapsedNanos: int64(time.Second + 6*time.Millisecond),
			FollowUpWallElapsedNanos: int64(2*time.Second - nextOffset), FollowUpMonotonicElapsedNanos: int64(2 * time.Second),
		}
		if _, err := Latencies(result); err == nil {
			t.Fatal("follow-up reading qualified an already rejected transport")
		}
	}
}
