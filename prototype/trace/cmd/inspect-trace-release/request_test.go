package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func validRequest() request {
	value := request{SchemaVersion: 1, ReleaseVersion: "0.1.0-dev.1", SourceCommit: strings.Repeat("a", 40), ImageDigest: "sha256:" + strings.Repeat("b", 64)}
	for _, role := range []string{"chart", "engine", "image", "programmes"} {
		value.Payloads = append(value.Payloads, payload{Role: role, Name: role + ".tar", Size: 10, SHA256: strings.Repeat("c", 64)})
	}
	return value
}

func TestRequestRejectsIncompleteAmbiguousOrUnboundedInputs(t *testing.T) {
	valid := validRequest()
	encoded, err := json.Marshal(valid)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := readRequest(bytes.NewReader(encoded)); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{
		string(encoded) + "{}", strings.Repeat(" ", 16385),
		strings.Replace(string(encoded), `"schemaVersion":1`, `"schemaVersion":1,"schemaVersion":1`, 1),
		strings.Replace(string(encoded), `"schemaVersion":1`, `"schemaVersion":1,"command":"run"`, 1),
	} {
		if _, err := readRequest(strings.NewReader(raw)); err == nil {
			t.Fatal("ambiguous or excessive input accepted")
		}
	}
	for name, change := range map[string]func(*request){
		"schema":    func(r *request) { r.SchemaVersion = 2 },
		"missing":   func(r *request) { r.Payloads = r.Payloads[:3] },
		"extra":     func(r *request) { r.Payloads = append(r.Payloads, r.Payloads[0]) },
		"role":      func(r *request) { r.Payloads[0].Role = "execute" },
		"path":      func(r *request) { r.Payloads[0].Name = "../chart.tar" },
		"absolute":  func(r *request) { r.Payloads[0].Name = "/chart.tar" },
		"duplicate": func(r *request) { r.Payloads[1].Name = r.Payloads[0].Name },
		"digest":    func(r *request) { r.Payloads[0].SHA256 = "mutable" },
		"size":      func(r *request) { r.Payloads[0].Size = 8<<20 + 1 },
		"zero":      func(r *request) { r.Payloads[0].Size = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			value := validRequest()
			change(&value)
			data, _ := json.Marshal(value)
			if _, err := readRequest(bytes.NewReader(data)); err == nil {
				t.Fatal("invalid payload request accepted")
			}
		})
	}
}

func TestPayloadFilesAreBoundedRegularSnapshotMembers(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "chart.tar"), []byte("0123456789"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("chart.tar", filepath.Join(dir, "link.tar")); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	item := validRequest().Payloads[0]
	file, err := openPayload(root, item)
	if err != nil {
		t.Fatal(err)
	}
	file.Close()
	for _, name := range []string{"link.tar", "missing.tar", "."} {
		item.Name = name
		if file, err := openPayload(root, item); err == nil {
			file.Close()
			t.Fatal("non-regular or missing member accepted")
		}
	}
	item.Name, item.Size = "chart.tar", 9
	if file, err := openPayload(root, item); err == nil {
		file.Close()
		t.Fatal("wrong-sized member accepted")
	}
}

func TestInvalidPayloadCannotProduceSuccessReceipt(t *testing.T) {
	dir := t.TempDir()
	value := validRequest()
	for _, item := range value.Payloads {
		if err := os.WriteFile(filepath.Join(dir, item.Name), []byte("0123456789"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	data, _ := json.Marshal(value)
	result, err := inspect(dir, bytes.NewReader(data))
	if err == nil || result.PayloadAgreement || result.Authenticated || result.RuntimeExecuted {
		t.Fatal("invalid archive produced successful inspection")
	}
}
