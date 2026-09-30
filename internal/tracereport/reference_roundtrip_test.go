package tracereport

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestReferenceRejectsUnserialisableUTCInstant(t *testing.T) {
	data, err := os.ReadFile("testdata/schema2-v2-file-loss.json")
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]json.RawMessage
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}
	for _, timestamp := range []string{"0000-01-01T00:00:00+01:00", "9999-12-31T23:59:59-01:00"} {
		value["capturedAt"], _ = json.Marshal(timestamp)
		input, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if ref, err := Describe(input); err == nil {
			if _, err := json.Marshal(ref); err != nil {
				t.Fatal("accepted reference cannot be serialised", err)
			}
		}
	}
}

func FuzzReferenceRoundTrip(f *testing.F) {
	paths, err := filepath.Glob("testdata/schema*.json")
	if err != nil {
		f.Fatal(err)
	}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(data)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		ref, err := Describe(data)
		if err != nil {
			return
		}
		encoded, err := json.Marshal(ref)
		if err != nil || len(encoded) > 4096 {
			t.Fatal("accepted reference cannot be serialised within its request bound", err)
		}
		var imported Reference
		if json.Unmarshal(encoded, &imported) != nil || imported != ref || VerifyReference(imported, data) != nil {
			t.Fatal("accepted reference cannot be read back and matched to its original bytes")
		}
	})
}
