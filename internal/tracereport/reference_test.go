package tracereport

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReferenceMatchesAllRetainedReportFixtures(t *testing.T) {
	files, err := filepath.Glob("testdata/*.json")
	if err != nil || len(files) < 18 {
		t.Fatal("missing compatibility fixtures", err)
	}
	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			data, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			ref, err := Describe(data)
			if err != nil || ref.Validate() != nil || VerifyReference(ref, data) != nil {
				t.Fatal("reference rejected valid retained report", err)
			}
			sum := sha256.Sum256(data)
			if ref.Digest != hex.EncodeToString(sum[:]) || ref.Bytes != len(data) || ref.Provenance != OperatorSupplied {
				t.Fatal("reference did not bind exact bytes with unverified operator provenance")
			}
			encoded, err := json.Marshal(ref)
			if err != nil {
				t.Fatal(err)
			}
			var imported Reference
			if json.Unmarshal(encoded, &imported) != nil || imported != ref {
				t.Fatal("reference did not round trip")
			}
			if VerifyReference(ref, append(bytes.Clone(data), '\n')) == nil {
				t.Fatal("changed original bytes matched reference")
			}
		})
	}
}

func TestReferencePreservesLossAndUnknownEvidence(t *testing.T) {
	data, _ := os.ReadFile("testdata/schema2-v2-file-loss.json")
	r, err := Describe(data)
	if err != nil || r.Coverage != "incomplete" || r.Termination != "expired" || !r.Lost.Known || r.Lost.Value != 1 || !r.Produced.Known || r.Produced.Value != 2 {
		t.Fatal("lost evidence changed", err)
	}
	data, _ = os.ReadFile("testdata/schema2-v2-partial-transport.json")
	r, err = Describe(data)
	if err != nil || r.Coverage != "unreported" || r.TransportComplete || r.Termination != "unreported" || r.Produced.Known || r.Lost.Known {
		t.Fatal("missing summary became measured evidence", err)
	}
}

func TestReferenceDropsImportedFreeText(t *testing.T) {
	data, _ := os.ReadFile("testdata/schema2-v2-file-loss.json")
	var doc map[string]any
	if json.Unmarshal(data, &doc) != nil {
		t.Fatal("fixture invalid")
	}
	private := "private-account-path-token-value"
	doc["toolVersion"] = private
	doc["caveats"] = []string{private}
	data, _ = json.Marshal(doc)
	r, err := Describe(data)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(r)
	if bytes.Contains(encoded, []byte(private)) || strings.Contains(fmt.Sprintf("%+v", r), r.Digest) {
		t.Fatal("free-form report fields or implicit formatting disclosed private data")
	}
	if VerifyReference(r, data) != nil {
		t.Fatal("reference does not identify original private file")
	}
	data[0] = '['
	if VerifyReference(r, data) == nil {
		t.Fatal("changed report accepted")
	}
}

func TestReferenceRejectsMissingUnknownsAndForgedClaims(t *testing.T) {
	data, _ := os.ReadFile("testdata/schema2-v2-file-loss.json")
	r, err := Describe(data)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(r)
	for name, mutate := range map[string]func(map[string]any){
		"false source authority": func(v map[string]any) { v["provenance"] = "server-verified" },
		"unbounded source":       func(v map[string]any) { v["bytes"] = MaxBytes + 1 },
		"future report":          func(v map[string]any) { v["reportSchemaVersion"] = 99 },
		"missing coverage":       func(v map[string]any) { delete(v, "coverage") },
		"missing loss":           func(v map[string]any) { delete(v, "lost") },
		"null loss":              func(v map[string]any) { v["lost"] = nil },
		"missing known state":    func(v map[string]any) { v["lost"] = map[string]any{"value": 0} },
		"unknown nonzero count":  func(v map[string]any) { v["lost"] = map[string]any{"known": false, "value": 1} },
		"loss hidden as complete": func(v map[string]any) {
			v["coverage"] = "complete"
		},
		"unreported completed stream": func(v map[string]any) { v["termination"] = "unreported" },
		"private extra field":         func(v map[string]any) { v["path"] = "/private/secret" },
	} {
		t.Run(name, func(t *testing.T) {
			var value map[string]any
			_ = json.Unmarshal(encoded, &value)
			mutate(value)
			input, _ := json.Marshal(value)
			var got Reference
			if json.Unmarshal(input, &got) == nil {
				t.Fatal("invalid reference accepted")
			}
		})
	}
	r.Lost.Value = 0
	if VerifyReference(r, data) == nil {
		t.Fatal("tampered evidence matched original report")
	}
}
