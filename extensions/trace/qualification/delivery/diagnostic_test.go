package delivery

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"
)

func TestSummaryDiagnosticsRespectProductionValidation(t *testing.T) {
	for _, state := range []string{"unavailable", "unknown-clock"} {
		t.Run(state, func(t *testing.T) {
			data, expected, deadline, now := fixture(t, fixturePath)
			lines := bytes.Split(bytes.TrimSpace(data), []byte("\n"))
			var terminal map[string]json.RawMessage
			if err := json.Unmarshal(lines[2], &terminal); err != nil {
				t.Fatal(err)
			}
			var summary map[string]json.RawMessage
			if err := json.Unmarshal(terminal["summary"], &summary); err != nil {
				t.Fatal(err)
			}
			if state == "unavailable" {
				summary["correlation"] = json.RawMessage(`{"state":"unavailable"}`)
			} else {
				summary["correlation"] = bytes.Replace(summary["correlation"], []byte(`"uncertaintyNanos":1000`), []byte(`"uncertaintyNanos":null`), 1)
			}
			terminal["summary"], _ = json.Marshal(summary)
			lines[2], _ = json.Marshal(terminal)
			data = append(bytes.Join(lines, []byte("\n")), '\n')
			result, err := Observe(bytes.NewReader(data), expected, now, func() (time.Time, error) { return deadline, nil })
			if state == "unknown-clock" {
				// An overlapping correlation with no clock is invalid at the
				// production frame boundary, before diagnostics may copy it.
				if err == nil || result.TransportComplete || result.SummaryDiagnostic != nil {
					t.Fatal("invalid frame escaped production validation")
				}
				return
			}
			if err == nil || result.TransportComplete || result.SummaryDiagnostic == nil {
				t.Fatal("rejected terminal evidence was lost or qualified")
			}
			if _, err := Latencies(result); err == nil {
				t.Fatal("diagnostic bypassed qualification")
			}
			d := result.SummaryDiagnostic
			if d.Termination != "expired" || d.Counts.Produced == nil || *d.Counts.Produced != 1 || d.EndedAfterReceiptNanos >= 0 {
				t.Fatal("numeric terminal evidence changed")
			}
			if state == "unavailable" && d.CorrelationState != "unavailable" {
				t.Fatal("correlation state missing")
			}
			raw, _ := json.Marshal(result)
			for _, secret := range []string{fixturePath, "private-pod", expected.SessionID, expected.EngineDigest} {
				if bytes.Contains(raw, []byte(secret)) {
					t.Fatal("diagnostic leaked frame identity")
				}
			}
		})
	}
}
