package incident

import (
	"encoding/json"
	"testing"
	"time"
)

func FuzzHistoryCaptureDecoder(f *testing.F) {
	at := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	c := historyCaptureFixture(f, at, "private-pod-uid")
	c.Changes.Markers = c.Changes.Markers[:1]
	f.Add([]byte(`{"schemaVersion":6}`))
	for _, sensitive := range []bool{false, true} {
		b, err := NewHistory(c, "test", at, sensitive)
		if err != nil {
			f.Fatal(err)
		}
		seed, err := json.Marshal(b)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > MaxHistoryBytes+1 {
			return
		}
		if b, err := decodeHistory(data); err == nil {
			if ValidateHistory(b) != nil {
				t.Fatal("decoder accepted invalid capture")
			}
		}
	})
}
