package volumehealth

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestCapabilityAndSourceStates(t *testing.T) {
	now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	for _, source := range []Source{PodSource, ControllerSource, BackendSource} {
		for _, a := range []Availability{Disabled, Unsupported, Unreported, Forbidden, Unavailable, Unknown} {
			o := Evaluate(Observation{Source: source, Availability: a, ObservedAt: now}, now)
			if o.State == StateHealthy || o.Adverse || o.ProbeFreshness != FreshnessUnknown {
				t.Fatalf("absent %s/%s became health: %+v", source, a, o)
			}
			if o.State != stateForAvailability(a) {
				t.Errorf("%s: state=%s", a, o.State)
			}
		}
		for _, status := range []Status{"", "FutureStatus"} {
			o := Observation{Source: source, Availability: Reported, ObservedAt: now, TransitionAt: now.Add(-365 * 24 * time.Hour)}
			if status != "" {
				o.Conditions = []Condition{{Status: status}}
			}
			o = Evaluate(o, now)
			if status == "" && o.State != StateHealthy {
				t.Errorf("explicit empty report: %+v", o)
			}
			if status != "" && (!o.Adverse || !o.UnknownStatus || o.State != StateAdverse || o.Conditions[0].Status != status) {
				t.Errorf("future status lost: %+v", o)
			}
			if o.ObservationFreshness != Fresh {
				t.Error("old transition is not an old probe")
			}
		}
	}
}

func TestKnownStatusesAndFreshness(t *testing.T) {
	now := time.Now().UTC()
	for source, statuses := range map[Source][]Status{PodSource: {"Inaccessible", "DataLoss", "Degraded"}, ControllerSource: {"Inaccessible", "DataLoss", "Degraded"}, BackendSource: {"StorageUnreachable", "StorageDegraded"}} {
		for _, status := range statuses {
			o := Evaluate(Observation{Source: source, Availability: Reported, ObservedAt: now, Conditions: []Condition{{Status: status}}}, now)
			if !o.Adverse || o.UnknownStatus || o.State != StateAdverse {
				t.Fatalf("known condition: %+v", o)
			}
			o = Evaluate(o, now.Add(3*time.Minute))
			if o.State != StateStale || !o.Adverse || o.ObservationFreshness != Stale {
				t.Fatalf("stale adversity lost: %+v", o)
			}
		}
	}
	for _, stamp := range []time.Time{{}, now.Add(time.Hour)} {
		o := Evaluate(Observation{Availability: Reported, ObservedAt: stamp}, now)
		if o.ObservationFreshness != FreshnessUnknown {
			t.Error("invalid clock became fresh")
		}
	}
}

func TestPrivateTextAndReportCopies(t *testing.T) {
	now := time.Now().UTC()
	o := Evaluate(Observation{Identity: Identity{Namespace: "private-namespace", PodName: "private-pod", PVCName: "private-claim", Driver: "private-driver"}, Source: PodSource, Availability: Reported, ObservedAt: now, Conditions: []Condition{{Status: "PrivateFutureStatus", Reason: strings.Repeat("é", 300), Message: "private-message\n" + strings.Repeat("界", 500)}}}, now)
	if len(o.Conditions[0].Reason) > 256 || len(o.Conditions[0].Message) > 1024 || !o.TextTruncated || !utf8.ValidString(o.Conditions[0].Message) || strings.Contains(o.Conditions[0].Message, "\n") {
		t.Fatal("text not bounded and normalised")
	}
	r := NewReport([]Observation{o})
	o.Conditions[0].Message = "changed-input"
	rows := r.Interactive()
	rows[0].Conditions[0].Message = "changed-output"
	if !strings.HasPrefix(r.Interactive()[0].Conditions[0].Message, "private-message") {
		t.Fatal("report shares mutable conditions")
	}
	for _, value := range []any{r, r.Interactive()[0], r.Interactive()[0].Identity, r.Interactive()[0].Conditions[0]} {
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(encoded), "private") || strings.Contains(string(encoded), "PrivateFuture") {
			t.Fatalf("private export: %s", encoded)
		}
	}
	if strings.Contains(fmt.Sprintf("%#v", r), "private") {
		t.Fatal("report debug output leaked private text")
	}
	if r.Summary().Adverse != 1 || r.Summary().UnknownStatus != 1 {
		t.Fatal("summary lost adverse counts")
	}
}
