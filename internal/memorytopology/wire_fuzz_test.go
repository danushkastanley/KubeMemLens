package memorytopology

import (
	"encoding/json"
	"testing"
	"unicode/utf8"
)

// Topology documents arrive from the Node-context producer and from saved
// captures. Whatever the bytes, decoding must refuse or stay inside its
// limits, and an accepted document must still be accepted after re-encoding.
func FuzzTopologyWire(f *testing.F) {
	for _, seed := range []string{"", "{}", "null", "[]", `{"schemaVersion":1}`, `{"unknown":true}`, `{"a":{"a":{"a":{"a":{}}}}}`, "{\"a\":\"\xff\"}"} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		var report Report
		if err := json.Unmarshal(data, &report); err == nil {
			if len(data) > MaxReportBytes || !utf8.Valid(data) {
				t.Fatalf("a report of %d bytes was accepted outside its limits", len(data))
			}
			encoded, err := json.Marshal(report)
			if err != nil {
				t.Fatalf("an accepted report does not encode: %v", err)
			}
			var again Report
			if err := json.Unmarshal(encoded, &again); err != nil {
				t.Fatalf("an accepted report is refused after re-encoding: %v\n%s", err, encoded)
			}
		}
		var observation Observation
		if err := json.Unmarshal(data, &observation); err == nil && (len(data) > MaxObservationBytes || !utf8.Valid(data)) {
			t.Fatalf("an observation of %d bytes was accepted outside its limits", len(data))
		}
	})
}
