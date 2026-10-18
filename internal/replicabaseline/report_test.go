package replicabaseline

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestReplicaReportRoundTripAndEvidenceValidation(t *testing.T) {
	input := maximumInput()
	report, err := Analyse(input)
	if err != nil {
		t.Fatal(err)
	}
	if err := report.Validate(); err != nil {
		t.Fatal("engine output rejected", err)
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Report
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if err := decoded.Validate(); err != nil {
		t.Fatal("roundtrip rejected", err)
	}
}

func TestReplicaReportRejectsAlteredEvidence(t *testing.T) {
	report, err := Analyse(maximumInput())
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(report)
	for _, change := range []string{"median", "score", "outlier", "foreign UID", "duplicate UID", "missing reference", "wrong unit", "wrong shape", "unknown reason"} {
		t.Run(change, func(t *testing.T) {
			var r Report
			if err := json.Unmarshal(encoded, &r); err != nil {
				t.Fatal(err)
			}
			c := &r.Peers[0].Comparisons[0]
			switch change {
			case "median":
				c.Distribution.Median++
			case "score":
				v := 2.0
				c.ModifiedScore = &v
			case "outlier":
				c.Outlier = "higher"
			case "foreign UID":
				c.References[0] = "other-namespace"
			case "duplicate UID":
				c.References[0] = c.References[1]
			case "missing reference":
				c.References = c.References[1:]
			case "wrong unit":
				c.Unit = "percent"
			case "wrong shape":
				r.Peers[0].Shape = strings.Repeat("f", 64)
			case "unknown reason":
				r.Peers[0].Exclusion = "safe"
			}
			if r.Validate() == nil {
				t.Fatal("tampered report accepted")
			}
		})
	}
}

func TestReplicaReportRejectsAmbiguousJSON(t *testing.T) {
	for _, body := range []string{`{"schemaVersion":1,"SchemaVersion":1}`, `{"unknown":true}`, `{"peers":` + strings.Repeat("[", 10) + strings.Repeat("]", 10) + `}`, `{} {}`} {
		var r Report
		if json.Unmarshal([]byte(body), &r) == nil {
			t.Fatal("invalid JSON accepted", body)
		}
	}
}
