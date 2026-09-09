package volumehealth

import (
	"testing"
	"time"
	"unicode/utf8"
)

func FuzzConditionText(f *testing.F) {
	f.Add("FutureStatus", "DriverReason", "message\nwith control characters")
	f.Add("Degraded", "reason", "界界界")
	f.Fuzz(func(t *testing.T, status, reason, message string) {
		now := time.Unix(1_000_000, 0)
		o := Evaluate(Observation{Source: PodSource, Availability: Reported, ObservedAt: now, Conditions: []Condition{{Status: Status(status), Reason: reason, Message: message}}}, now)
		c := o.Conditions[0]
		if !o.Adverse || o.State != StateAdverse || string(c.Status) != status {
			t.Fatal("condition evidence was lost")
		}
		if len(c.Message) > 1024 || len(c.Reason) > 256 || !utf8.ValidString(c.Message) || !utf8.ValidString(c.Reason) {
			t.Fatal("text bound or UTF-8 invariant failed")
		}
	})
}
