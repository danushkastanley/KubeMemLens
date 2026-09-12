package nodecontext

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/capability"
)

func validObservation(at time.Time) Observation {
	zero := uint64(0)
	return Observation{NodeName: "node-a", NodeUID: "uid-a", ReportedAt: at, Availability: capability.Available,
		Evidence: capability.Envelope{Source: Source, APIVersion: "v1alpha1", CapturedAt: at, ReceivedAt: at,
			Scope: capability.NodeScope, Freshness: capability.Fresh, Completeness: capability.Partial, Stability: capability.ImplementationSpecific},
		Stats: &Stats{StartedAt: at.Add(-time.Hour), Provenance: Unknown, Memory: &Memory{CapturedAt: at, UsageBytes: &zero}}}
}

func TestValidateObservationTrustBoundaries(t *testing.T) {
	now := time.Now().UTC()
	for name, change := range map[string]func(*Observation){
		"incomplete fields marked complete": func(o *Observation) { o.Evidence.Completeness = capability.Complete },
		"valid measured zero":               func(*Observation) {},
		"sample future":                     func(o *Observation) { o.Stats.Memory.CapturedAt = now.Add(time.Minute) },
		"sample stale":                      func(o *Observation) { o.Stats.Memory.CapturedAt = now.Add(-3 * time.Minute) },
		"forged source time":                func(o *Observation) { o.Evidence.CapturedAt = now.Add(-time.Second) },
		"unknown system name":               func(o *Observation) { o.Stats.SystemContainers = []SystemContainer{{Category: "secret-pod"}} },
		"private caveat":                    func(o *Observation) { o.Evidence.Caveats = []string{"secret-namespace"} },
		"failure with measurements":         func(o *Observation) { o.Availability = capability.Unavailable; o.Reason = TimedOut },
		"timestamp without measurement":     func(o *Observation) { o.Stats.Memory.UsageBytes = nil },
		"fake healthy nil":                  func(o *Observation) { o.Stats = nil },
		"invalid PSI":                       func(o *Observation) { o.Stats.Memory.PSI = &PSI{Some: PSIData{Avg10: 101}} },
		"before boot":                       func(o *Observation) { o.Stats.StartedAt = now.Add(time.Second) },
	} {
		t.Run(name, func(t *testing.T) {
			value := validObservation(now)
			change(&value)
			err := Validate(value, now, 2*time.Minute, 30*time.Second)
			if (err == nil) != (name == "valid measured zero") {
				t.Fatalf("validation = %v", err)
			}
		})
	}
}

func TestStrictObservationDecoding(t *testing.T) {
	data, err := json.Marshal(validObservation(time.Now().UTC()))
	if err != nil {
		t.Fatal(err)
	}
	var result Observation
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	cases := []string{
		strings.Replace(string(data), `"nodeName"`, `"NodeName"`, 1),
		strings.Replace(string(data), `"memory":{`, `"memory":{"psi":{"some":{},"full":{}},`, 1),
		strings.Replace(string(data), `"memory":{`, `"memory":{"psi":{"some":null,"full":null},`, 1),
		strings.Replace(string(data), `"nodeName":"node-a"`, `"nodeName":"node-a","nodeName":"node-b"`, 1),
		strings.Replace(string(data), `"nodeName":"node-a"`, `"podName":"secret"`, 1),
		strings.Replace(string(data), `"stats":{`, `"stats":{"systemContainers":[{},{},{},{},{}],`, 1),
		strings.Replace(string(data), `"evidence":{`, `"evidence":{"caveats":[`+strings.Repeat(`"x",`, 16)+`"x"],`, 1),
		`{"nodeName":"` + strings.Repeat("x", MaxObservationBytes) + `"}`,
		string(data) + `{}`,
	}
	for _, data := range cases {
		if err := json.Unmarshal([]byte(data), &result); err == nil {
			t.Fatal("invalid wire observation accepted")
		}
	}
}

func FuzzObservationDecoder(f *testing.F) {
	data, _ := json.Marshal(validObservation(time.Unix(1800000000, 0).UTC()))
	f.Add(data)
	f.Fuzz(func(t *testing.T, data []byte) {
		var value Observation
		if err := json.Unmarshal(data, &value); err != nil {
			return
		}
		_ = Validate(value, time.Unix(1800000000, 0).UTC(), 2*time.Minute, 30*time.Second)
	})
}
